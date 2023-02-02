package tasks

import (
	exampletasks "github.com/RichardKnop/machinery/example/tasks"
	"github.com/RichardKnop/machinery/v1"
	"github.com/RichardKnop/machinery/v1/config"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/pkg/errors"
	"reflect"
	"runtime"
)

var registeredTasks map[string]any

func init() {
	registeredTasks = make(map[string]any)
	RegisterTask("add", exampletasks.Add)
	RegisterTask("multiply", exampletasks.Multiply)
	RegisterTask("sum_ints", exampletasks.SumInts)
	RegisterTask("sum_floats", exampletasks.SumFloats)
	RegisterTask("concat", exampletasks.Concat)
	RegisterTask("split", exampletasks.Split)
	RegisterTask("panic_task", exampletasks.PanicTask)
	RegisterTask("long_running_task", exampletasks.LongRunningTask)
}

func StartServer(tasksConfig Config) (server *machinery.Server, err error) {
	cnf := &config.Config{
		DefaultQueue:    tasksConfig.TasksDefaultQueue(),
		ResultsExpireIn: tasksConfig.TasksResultsExpireIn(),
		Broker:          tasksConfig.TasksBroker(),
		ResultBackend:   tasksConfig.TasksResultBackend(),
		Redis: &config.RedisConfig{
			MaxIdle:                tasksConfig.TasksRedis().RedisMaxIdle(),
			IdleTimeout:            tasksConfig.TasksRedis().RedisIdleTimeout(),
			ReadTimeout:            tasksConfig.TasksRedis().RedisReadTimeout(),
			WriteTimeout:           tasksConfig.TasksRedis().RedisWriteTimeout(),
			ConnectTimeout:         tasksConfig.TasksRedis().RedisConnectTimeout(),
			NormalTasksPollPeriod:  tasksConfig.TasksRedis().RedisNormalTasksPollPeriod(),
			DelayedTasksPollPeriod: tasksConfig.TasksRedis().RedisDelayedTasksPollPeriod(),
		},
	}

	if server, err = machinery.NewServer(cnf); err != nil {
		err = errors.Wrap(err, "could not create new machinery server")
		return
	}

	for name, signature := range tasksConfig.TasksPeriodicTaskSignatures() {
		if err = server.RegisterPeriodicTask(signature.PeriodicTaskSignatureCron(), name, &tasks.Signature{
			Name:       name,
			Args:       signature.PeriodicTaskSignatureArgs(),
			RetryCount: signature.PeriodicTaskSignatureRetryCount(),
		}); err != nil {
			err = errors.Wrapf(err, "could not register PeriodicTask %q with spec %v", name, signature)
			return
		}
	}

	if err = server.RegisterTasks(registeredTasks); err != nil {
		err = errors.Wrapf(err, "could not register %d tasks", len(registeredTasks))
	}

	log.INFO.Printf("Tasks that were registered:")
	for i, name := range server.GetRegisteredTaskNames() {
		task, _ := server.GetRegisteredTask(name)
		log.INFO.Printf("\tTask %d: %v", i+1, runtime.FuncForPC(reflect.ValueOf(task).Pointer()).Name())
	}
	return
}

func RegisterTask(name string, function any) {
	registeredTasks[name] = function
}
