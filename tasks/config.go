package tasks

import "github.com/RichardKnop/machinery/v1/tasks"

type RedisConfig interface {
	RedisMaxIdle() int
	RedisIdleTimeout() int
	RedisReadTimeout() int
	RedisWriteTimeout() int
	RedisConnectTimeout() int
	RedisNormalTasksPollPeriod() int
	RedisDelayedTasksPollPeriod() int
}

type PeriodicTaskSignature interface {
	PeriodicTaskSignatureArgs() []tasks.Arg
	PeriodicTaskSignatureCron() string
	PeriodicTaskSignatureRetryCount() int
}

type Config interface {
	TasksDefaultQueue() string
	TasksResultsExpireIn() int
	TasksBroker() string
	TasksResultBackend() string
	TasksRedis() RedisConfig
	TasksPeriodicTaskSignatures() map[string]PeriodicTaskSignature
	TasksPeriodicTaskSignature(taskName string) PeriodicTaskSignature
}
