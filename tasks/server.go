package tasks

import (
	exampletasks "github.com/RichardKnop/machinery/example/tasks"
	"github.com/RichardKnop/machinery/v1"
	"github.com/RichardKnop/machinery/v1/config"
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
	RegisterTask("twitter_search_test", TwitterClientTest)
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
	return server, server.RegisterTasks(registeredTasks)
}

func RegisterTask(name string, function any) {
	registeredTasks[name] = function
}
