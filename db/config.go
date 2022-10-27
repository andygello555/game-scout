package db

import "fmt"

type Config interface {
	DBHost() string
	DBUser() string
	DBPassword() string
	DBName() string
	TestDBName() string
	DBPort() int
	DBSSLMode() string
	DBTimezone() string
}

const (
	PostgresDBName = "postgres"
)

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

func configToTestDSN(config Config) string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s",
		config.DBHost(),
		config.DBUser(),
		config.DBPassword(),
		config.TestDBName(),
		config.DBPort(),
		config.DBSSLMode(),
		config.DBTimezone(),
	)
}

func configToPostgresDSN(config Config) string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s",
		config.DBHost(),
		config.DBUser(),
		config.DBPassword(),
		PostgresDBName,
		config.DBPort(),
		config.DBSSLMode(),
		config.DBTimezone(),
	)
}
