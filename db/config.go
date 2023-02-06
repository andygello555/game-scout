package db

import "fmt"

type RWConfig interface {
	ID() int
	R() bool
	W() bool
}

type Config interface {
	DBHost() string
	DBUser() string
	DBPassword() string
	DBName() string
	TestDBName() string
	DBPort() int
	DBSSLMode() string
	DBTimezone() string
	DBPostgresDBName() string
	DBDefaultRWAccess() (config RWConfig)
	DBRWAccessConfigForID(id int) (config RWConfig)
	DBRWAccessForID(id int) (read bool, write bool)
	DBPhaseReadAccess(id int) bool
	DBPhaseWriteAccess(id int) bool
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
		config.DBPostgresDBName(),
		config.DBPort(),
		config.DBSSLMode(),
		config.DBTimezone(),
	)
}
