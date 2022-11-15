package main

import (
	"fmt"
	"github.com/RichardKnop/logging"
	"github.com/RichardKnop/machinery/example/tracers"
	"github.com/RichardKnop/machinery/v1"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/RichardKnop/machinery/v1/tasks"
	task "github.com/andygello555/game-scout/tasks"
	"github.com/pkg/errors"
	"os/exec"
	"sort"
	"strings"
	"time"
)

var (
	taskStartTable          map[string]*time.Time
	taskTotalRuntimeTable   map[string]time.Duration
	taskAverageRuntimeTable map[string]time.Duration
	taskCountTable          map[string]int64
)

// throughWriter is a writer that writes to the logger (logging.LoggerInterface), by converting the given
// bytes to a string.
type throughWriter struct {
	logger logging.LoggerInterface
}

func (w *throughWriter) Write(d []byte) (int, error) {
	dString := string(d[:])
	w.logger.Print(strings.TrimSpace(dString))
	return len(d), nil
}

func worker() (err error) {
	consumerTag := "machinery_worker"

	var cleanup func()
	if cleanup, err = tracers.SetupTracer(consumerTag); err != nil {
		log.FATAL.Fatalln("Unable to instantiate a tracer:", err)
	}
	defer cleanup()

	var server *machinery.Server
	if server, err = task.StartServer(globalConfig.Tasks); err != nil {
		return err
	}

	// Create the ScoutWebPipes co-process
	scoutWebPipes := exec.Command(globalConfig.SteamWebPipes.BinaryLocation)
	// Hook the stdout to the INFO level logger
	scoutWebPipes.Stdout = &throughWriter{logger: log.INFO}
	// Start the ScoutWebPipes process
	if err = scoutWebPipes.Start(); err != nil {
		return errors.Wrapf(
			err,
			"cannot start ScoutWebPipes (%s) process",
			globalConfig.SteamWebPipes.BinaryLocation,
		)
	}
	// Defer a function that will kill the scoutWebPipes process
	defer func() {
		if err = scoutWebPipes.Process.Kill(); err != nil {
			err = errors.Wrapf(
				err,
				"cannot kill ScoutWebPipes (%s) process",
				globalConfig.SteamWebPipes.BinaryLocation,
			)
		}
	}()

	// The second argument is a consumer tag
	// Ideally, each worker should have a unique tag (worker1, worker2 etc)
	w := server.NewWorker(consumerTag, 0)

	// Will construct a call signature for the given tasks.Signature with the arg name, value, and type
	constructCallSignature := func(signature *tasks.Signature) string {
		var b strings.Builder
		for i, arg := range signature.Args {
			argPrefix := ""
			if arg.Name != "" {
				argPrefix = fmt.Sprintf("%s: ", arg.Name)
			}
			b.WriteString(fmt.Sprintf("%s%v (%s)", argPrefix, arg.Value, arg.Type))
			if i != len(signature.Args)-1 {
				b.WriteString(", ")
			}
		}
		return fmt.Sprintf("%s(%s)", signature.Name, b.String())
	}

	// We have a table that holds the start times of each running task
	taskStartTable = make(map[string]*time.Time)

	// Another table for recording the total runtime of tasks that have been run to completion
	taskTotalRuntimeTable = make(map[string]time.Duration)

	// We also have a table that records the average runtime for tasks
	taskAverageRuntimeTable = make(map[string]time.Duration)

	// And finally a table that records the number of times a task has been run to completion
	taskCountTable = make(map[string]int64)

	// Here we inject some custom code for error handling,
	// start and end of task hooks, useful for metrics for example.
	errorHandler := func(err error) {
		log.ERROR.Printf("Error occurred when executing task:\n\t%v\n", err)
	}

	preTaskHandler := func(signature *tasks.Signature) {
		startTime := time.Now().UTC()
		taskStartTable[signature.UUID] = &startTime
		log.INFO.Printf("Starting %s: %s\n", signature.UUID, constructCallSignature(signature))
	}

	postTaskHandler := func(signature *tasks.Signature) {
		startTime, ok := taskStartTable[signature.UUID]
		endTime := time.Now().UTC()
		if ok {
			duration := endTime.Sub(*startTime)
			taskCountTable[signature.Name] += 1
			taskTotalRuntimeTable[signature.Name] += duration
			taskAverageRuntimeTable[signature.Name] = taskTotalRuntimeTable[signature.Name] / time.Duration(taskCountTable[signature.Name])
			log.INFO.Printf("Finished %s: %s, in %s\n", signature.Name, constructCallSignature(signature), duration.String())
			delete(taskStartTable, signature.UUID)
		} else {
			log.INFO.Printf("Finished %s: %s\n", signature.Name, constructCallSignature(signature))
		}

		// We also display the average task runtimes as a sorted descending list
		log.INFO.Println("Average task runtimes:")
		durations := make([]struct {
			name     string
			duration time.Duration
		}, 0, len(taskAverageRuntimeTable))
		for name, duration := range taskAverageRuntimeTable {
			durations = append(durations, struct {
				name     string
				duration time.Duration
			}{name: name, duration: duration})
		}
		sort.Slice(durations, func(i, j int) bool {
			return durations[i].duration > durations[j].duration
		})

		for i, duration := range durations {
			log.INFO.Printf("%d: %s - %s", i+1, duration.name, duration.duration)
		}
	}

	w.SetPostTaskHandler(postTaskHandler)
	w.SetErrorHandler(errorHandler)
	w.SetPreTaskHandler(preTaskHandler)

	return w.Launch()
}
