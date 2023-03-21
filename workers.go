package main

import (
	"fmt"
	"github.com/RichardKnop/logging"
	"github.com/RichardKnop/machinery/example/tracers"
	"github.com/RichardKnop/machinery/v1"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/RichardKnop/machinery/v1/tasks"
	myErrors "github.com/andygello555/game-scout/errors"
	task "github.com/andygello555/game-scout/tasks"
	"github.com/pkg/errors"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	taskStartTable     map[string]*time.Time
	taskStartTableSync sync.RWMutex

	taskTotalRuntimeTable     map[string]time.Duration
	taskTotalRuntimeTableSync sync.RWMutex

	taskAverageRuntimeTable     map[string]time.Duration
	taskAverageRuntimeTableSync sync.RWMutex

	taskCountTable     map[string]int64
	taskCountTableSync sync.RWMutex
)

// throughWriter is a writer that writes to the logger (logging.LoggerInterface), by converting the given
// bytes to a string.
type throughWriter struct {
	logger logging.LoggerInterface
}

var timePrefixPattern = regexp.MustCompile(`^(\[.*?] ).*$`)

func (w *throughWriter) Write(d []byte) (int, error) {
	dString := strings.TrimSpace(string(d[:]))
	matches := timePrefixPattern.FindStringSubmatch(dString)
	if len(matches) > 0 {
		dString = strings.TrimPrefix(dString, matches[1])
	}
	w.logger.Print(dString)
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

	if !globalConfig.SteamWebPipes.Disable {
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

		// The SteamAppWebsocketScraper will handle all the changelists pushed to the websocket by ScoutWebPipes, by
		// scraping and saving each one as a SteamApp.
		websocketScraper := NewSteamAppWebsocketScraper(1, globalConfig)

		// Defer a function that will kill the scoutWebPipes process. We also stop the SteamAppWebsocketScraper and wait for
		// it to gracefully exit.
		defer func() {
			log.INFO.Printf("Stopping ScoutWebPipes and waiting for SteamAppWebsocketScraper")
			err = myErrors.MergeErrors(err, errors.Wrapf(
				scoutWebPipes.Process.Kill(),
				"cannot kill ScoutWebPipes (%s) process",
				globalConfig.SteamWebPipes.BinaryLocation,
			))
			_, waitErr := scoutWebPipes.Process.Wait()
			err = myErrors.MergeErrors(err, errors.Wrap(waitErr, "wait failed"))
			err = myErrors.MergeErrors(err, errors.Wrap(websocketScraper.Stop(), "could not stop SteamAppWebsocketScraper"))
		}()

		// Start the SteamAppWebsocketScraper
		websocketScraper.Start()
	}

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
	taskStartTableSync = sync.RWMutex{}

	// Another table for recording the total runtime of tasks that have been run to completion
	taskTotalRuntimeTable = make(map[string]time.Duration)
	taskTotalRuntimeTableSync = sync.RWMutex{}

	// We also have a table that records the average runtime for tasks
	taskAverageRuntimeTable = make(map[string]time.Duration)
	taskAverageRuntimeTableSync = sync.RWMutex{}

	// And finally a table that records the number of times a task has been run to completion
	taskCountTable = make(map[string]int64)
	taskCountTableSync = sync.RWMutex{}

	// Here we inject some custom code for error handling,
	// start and end of task hooks, useful for metrics for example.
	errorHandler := func(err error) {
		log.ERROR.Printf("Error occurred when executing task:\n\t%v\n", err)
	}

	preTaskHandler := func(signature *tasks.Signature) {
		startTime := time.Now().UTC()

		taskStartTableSync.Lock()
		taskStartTable[signature.UUID] = &startTime
		taskStartTableSync.Unlock()

		log.INFO.Printf("Starting %s: %s\n", signature.UUID, constructCallSignature(signature))
	}

	postTaskHandler := func(signature *tasks.Signature) {
		taskStartTableSync.RLock()
		startTime, ok := taskStartTable[signature.UUID]
		taskStartTableSync.RUnlock()

		endTime := time.Now().UTC()
		if ok {
			duration := endTime.Sub(*startTime)

			taskCountTableSync.Lock()
			taskCountTable[signature.Name] += 1
			taskCountTableSync.Unlock()

			taskTotalRuntimeTableSync.Lock()
			taskTotalRuntimeTable[signature.Name] += duration
			taskTotalRuntimeTableSync.Unlock()

			taskCountTableSync.RLock()
			taskCount := taskCountTable[signature.Name]
			taskCountTableSync.RUnlock()

			taskAverageRuntimeTableSync.Lock()
			taskAverageRuntimeTable[signature.Name] = duration / time.Duration(taskCount)
			taskAverageRuntimeTableSync.Unlock()

			log.INFO.Printf("Continue %s: %s, in %s\n", signature.Name, constructCallSignature(signature), duration.String())

			taskStartTableSync.Lock()
			delete(taskStartTable, signature.UUID)
			taskStartTableSync.Unlock()
		} else {
			log.INFO.Printf("Continue %s: %s\n", signature.Name, constructCallSignature(signature))
		}

		// We also display the average task runtimes as a sorted descending list
		log.INFO.Println("Average task runtimes:")
		taskAverageRuntimeTableSync.RLock()
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
		taskAverageRuntimeTableSync.RUnlock()

		// Sort the durations
		sort.Slice(durations, func(i, j int) bool {
			return durations[i].duration > durations[j].duration
		})

		// Print the durations in descending order
		for i, duration := range durations {
			log.INFO.Printf("%d: %s - %s", i+1, duration.name, duration.duration)
		}
	}

	w.SetPostTaskHandler(postTaskHandler)
	w.SetErrorHandler(errorHandler)
	w.SetPreTaskHandler(preTaskHandler)

	return w.Launch()
}
