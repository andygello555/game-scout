package tasks

import (
	exampletasks "github.com/RichardKnop/machinery/example/tasks"
	"github.com/RichardKnop/machinery/v1"
	"github.com/RichardKnop/machinery/v1/config"
	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/pkg/errors"
	"reflect"
)

type periodicRegisteredTask struct {
	*tasks.Signature
	Spec string
}

var registeredTasks map[string]any
var periodicRegisteredTasks map[string]periodicRegisteredTask

func init() {
	registeredTasks = make(map[string]any)
	periodicRegisteredTasks = make(map[string]periodicRegisteredTask)
	RegisterTask("add", exampletasks.Add)
	RegisterTask("multiply", exampletasks.Multiply)
	RegisterTask("sum_ints", exampletasks.SumInts)
	RegisterTask("sum_floats", exampletasks.SumFloats)
	RegisterTask("concat", exampletasks.Concat)
	RegisterTask("split", exampletasks.Split)
	RegisterTask("panic_task", exampletasks.PanicTask)
	RegisterTask("long_running_task", exampletasks.LongRunningTask)
}

func StartServer(tasksConfig Config) (*machinery.Server, error) {
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

	server, err := machinery.NewServer(cnf)
	if err != nil {
		return nil, err
	}

	for name, signature := range periodicRegisteredTasks {
		if err = server.RegisterPeriodicTask(signature.Spec, name, signature.Signature); err != nil {
			return nil, errors.Wrapf(err, "could not register PeriodicTask %q with spec %q", name, signature.Spec)
		}
	}
	return server, server.RegisterTasks(registeredTasks)
}

func RegisterTask(name string, function any) {
	registeredTasks[name] = function
}

func RegisterPeriodicTask(cron, name string, args ...any) {
	periodicRegisteredTasks[name] = periodicRegisteredTask{
		Signature: &tasks.Signature{
			Name: name,
			Args: slices.Comprehension(args, func(idx int, value any, arr []any) tasks.Arg {
				return tasks.Arg{
					Type:  reflect.TypeOf(value).String(),
					Value: value,
				}
			}),
		},
		Spec: cron,
	}
}
