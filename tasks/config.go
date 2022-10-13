package tasks

type RedisConfig interface {
	RedisMaxIdle() int
	RedisIdleTimeout() int
	RedisReadTimeout() int
	RedisWriteTimeout() int
	RedisConnectTimeout() int
	RedisNormalTasksPollPeriod() int
	RedisDelayedTasksPollPeriod() int
}

type Config interface {
	TasksDefaultQueue() string
	TasksResultsExpireIn() int
	TasksBroker() string
	TasksResultBackend() string
	TasksRedis() RedisConfig
}
