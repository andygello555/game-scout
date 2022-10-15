package main

import (
	"errors"
	"fmt"
	"github.com/RichardKnop/machinery/v1/backends/result"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/andygello555/game-scout/db"
	task "github.com/andygello555/game-scout/tasks"
	"github.com/andygello555/game-scout/twitter"
	"github.com/urfave/cli"
	"os"
	"reflect"
	"strconv"
	"time"
)

var (
	app *cli.App
)

func init() {
	// Initialise a CLI app
	app = cli.NewApp()
	app.Name = "game-scout"
	app.Usage = "the game-scout worker"
	app.Version = "0.0.0"
	if err := LoadConfig(); err != nil {
		panic(err)
	}
}

func main() {
	// We set up the global DB instance...
	if err := db.Open(globalConfig.DB); err != nil {
		panic(err)
	}
	defer db.Close()

	// Set up the twitter API client
	if err := twitter.ClientCreate(globalConfig.Twitter); err != nil {
		panic(err)
	}

	// Set the CLI app commands
	app.Commands = []cli.Command{
		{
			Name:  "worker",
			Usage: "launch game-scout worker",
			Action: func(c *cli.Context) error {
				if err := worker(); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				return nil
			},
		},
		{
			Name:  "send",
			Usage: "send example tasks",
			Action: func(c *cli.Context) error {
				if !c.Args().Present() {
					if err := sendAll(); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					return nil
				} else {
					if err := sendOne(c.Args().First(), c.Args().Tail()...); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					return nil
				}
			},
		},
		{
			Name:  "tweetcap",
			Usage: "gets the Tweet cap from the twitter client",
			Action: func(c *cli.Context) error {
				fmt.Println(twitter.Client.TweetCap)
				return nil
			},
		},
	}

	// Run the CLI app
	err := app.Run(os.Args)
	if err != nil {
		return
	}
}

func sendOne(taskName string, args ...string) error {
	var err error
	var broker *task.Broker
	if broker, err = task.NewBroker(globalConfig.Tasks); err != nil {
		return err
	}
	defer broker.Cleanup()

	// Parse the arguments to a list of tasks.Args
	sigArgs := make([]tasks.Arg, len(args))
	for i, arg := range args {
		var parsed any
		var ok bool
		var typeName string
		var convert func(parse string) (any, bool)
		for typeName, convert = range map[string]func(parse string) (any, bool){
			"float64": func(parse string) (any, bool) {
				if value, err := strconv.ParseFloat(parse, 64); err != nil {
					return nil, false
				} else {
					return value, true
				}
			},
			"int64": func(parse string) (any, bool) {
				if value, err := strconv.ParseInt(parse, 10, 64); err != nil {
					return nil, false
				} else {
					return value, true
				}
			},
			"string": func(parse string) (any, bool) {
				return parse, true
			},
		} {
			if parsed, ok = convert(arg); ok {
				break
			}
		}
		sigArgs[i] = tasks.Arg{
			Value: parsed,
			Type:  typeName,
		}
	}

	// Construct the signature
	signature := tasks.Signature{
		Name: taskName,
		Args: sigArgs,
	}

	var asyncResult *result.AsyncResult
	asyncResult, err = broker.SendTaskWithContext(&signature)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	var results []reflect.Value
	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("Results: %v\n", tasks.HumanReadableResults(results))
	return nil
}

func sendAll() error {
	var (
		addTask0, addTask1, addTask2                      tasks.Signature
		multiplyTask0, multiplyTask1                      tasks.Signature
		sumIntsTask, sumFloatsTask, concatTask, splitTask tasks.Signature
		panicTask                                         tasks.Signature
		longRunningTask                                   tasks.Signature
	)

	var initTasks = func() {
		addTask0 = tasks.Signature{
			Name: "add",
			Args: []tasks.Arg{
				{
					Type:  "int64",
					Value: 1,
				},
				{
					Type:  "int64",
					Value: 1,
				},
			},
		}

		addTask1 = tasks.Signature{
			Name: "add",
			Args: []tasks.Arg{
				{
					Type:  "int64",
					Value: 2,
				},
				{
					Type:  "int64",
					Value: 2,
				},
			},
		}

		addTask2 = tasks.Signature{
			Name: "add",
			Args: []tasks.Arg{
				{
					Type:  "int64",
					Value: 5,
				},
				{
					Type:  "int64",
					Value: 6,
				},
			},
		}

		multiplyTask0 = tasks.Signature{
			Name: "multiply",
			Args: []tasks.Arg{
				{
					Type:  "int64",
					Value: 4,
				},
			},
		}

		multiplyTask1 = tasks.Signature{
			Name: "multiply",
		}

		sumIntsTask = tasks.Signature{
			Name: "sum_ints",
			Args: []tasks.Arg{
				{
					Type:  "[]int64",
					Value: []int64{1, 2},
				},
			},
		}

		sumFloatsTask = tasks.Signature{
			Name: "sum_floats",
			Args: []tasks.Arg{
				{
					Type:  "[]float64",
					Value: []float64{1.5, 2.7},
				},
			},
		}

		concatTask = tasks.Signature{
			Name: "concat",
			Args: []tasks.Arg{
				{
					Type:  "[]string",
					Value: []string{"foo", "bar"},
				},
			},
		}

		splitTask = tasks.Signature{
			Name: "split",
			Args: []tasks.Arg{
				{
					Type:  "string",
					Value: "foo",
				},
			},
		}

		panicTask = tasks.Signature{
			Name: "panic_task",
		}

		longRunningTask = tasks.Signature{
			Name: "long_running_task",
		}
	}

	var err error
	var broker *task.Broker
	if broker, err = task.NewBroker(globalConfig.Tasks); err != nil {
		return err
	}
	defer broker.Cleanup()

	log.INFO.Println("Starting batch:", broker.BatchID)
	/*
	* First, let's try sending a single task
	 */
	initTasks()

	log.INFO.Println("Single task:")

	asyncResult, err := broker.SendTaskWithContext(&addTask0)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err := asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("1 + 1 = %v\n", tasks.HumanReadableResults(results))

	/*
	* Try couple of tasks with a slice argument and slice return value
	 */
	asyncResult, err = broker.SendTaskWithContext(&sumIntsTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("sum([1, 2]) = %v\n", tasks.HumanReadableResults(results))

	asyncResult, err = broker.SendTaskWithContext(&sumFloatsTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("sum([1.5, 2.7]) = %v\n", tasks.HumanReadableResults(results))

	asyncResult, err = broker.SendTaskWithContext(&concatTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("concat([\"foo\", \"bar\"]) = %v\n", tasks.HumanReadableResults(results))

	asyncResult, err = broker.SendTaskWithContext(&splitTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("split([\"foo\"]) = %v\n", tasks.HumanReadableResults(results))

	/*
	* Now let's explore ways of sending multiple tasks
	 */

	// Now let's try a parallel execution
	initTasks()
	log.INFO.Println("Group of tasks (parallel execution):")

	group, err := tasks.NewGroup(&addTask0, &addTask1, &addTask2)
	if err != nil {
		return fmt.Errorf("Error creating group: %s", err.Error())
	}

	asyncResults, err := broker.SendGroupWithContext(group, 10)
	if err != nil {
		return fmt.Errorf("Could not send group: %s", err.Error())
	}

	for _, asyncResult := range asyncResults {
		results, err = asyncResult.Get(time.Millisecond * 5)
		if err != nil {
			return fmt.Errorf("Getting task result failed with error: %s", err.Error())
		}
		log.INFO.Printf(
			"%v + %v = %v\n",
			asyncResult.Signature.Args[0].Value,
			asyncResult.Signature.Args[1].Value,
			tasks.HumanReadableResults(results),
		)
	}

	// Now let's try a group with a chord
	initTasks()
	log.INFO.Println("Group of tasks with a callback (chord):")

	group, err = tasks.NewGroup(&addTask0, &addTask1, &addTask2)
	if err != nil {
		return fmt.Errorf("Error creating group: %s", err.Error())
	}

	chord, err := tasks.NewChord(group, &multiplyTask1)
	if err != nil {
		return fmt.Errorf("Error creating chord: %s", err)
	}

	chordAsyncResult, err := broker.SendChordWithContext(chord, 10)
	if err != nil {
		return fmt.Errorf("Could not send chord: %s", err.Error())
	}

	results, err = chordAsyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting chord result failed with error: %s", err.Error())
	}
	log.INFO.Printf("(1 + 1) * (2 + 2) * (5 + 6) = %v\n", tasks.HumanReadableResults(results))

	// Now let's try chaining task results
	initTasks()
	log.INFO.Println("Chain of tasks:")

	chain, err := tasks.NewChain(&addTask0, &addTask1, &addTask2, &multiplyTask0)
	if err != nil {
		return fmt.Errorf("Error creating chain: %s", err)
	}

	chainAsyncResult, err := broker.SendChainWithContext(chain)
	if err != nil {
		return fmt.Errorf("Could not send chain: %s", err.Error())
	}

	results, err = chainAsyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting chain result failed with error: %s", err.Error())
	}
	log.INFO.Printf("(((1 + 1) + (2 + 2)) + (5 + 6)) * 4 = %v\n", tasks.HumanReadableResults(results))

	// Let's try a task which throws panic to make sure stack trace is not lost
	initTasks()
	asyncResult, err = broker.SendTaskWithContext(&panicTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	_, err = asyncResult.Get(time.Millisecond * 5)
	if err == nil {
		return errors.New("Error should not be nil if task panicked")
	}
	log.INFO.Printf("Task panicked and returned error = %v\n", err.Error())

	// Let's try a long running task
	initTasks()
	asyncResult, err = broker.SendTaskWithContext(&longRunningTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting long running task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("Long running task returned = %v\n", tasks.HumanReadableResults(results))

	return nil
}
