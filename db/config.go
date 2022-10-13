package db

import "fmt"

type Config interface {
	DBHost() string
	DBUser() string
	DBPassword() string
	DBName() string
	DBPort() int
	DBSSLMode() string
	DBTimezone() string
}

func configToDSN(config Config) string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s",
		config.DBHost(),
		config.DBUser(),
		config.DBPassword(),
		config.DBName(),
		config.DBPort(),
		config.DBSSLMode(),
		config.DBTimezone(),
	)
}
