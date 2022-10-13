package db

import (
	"database/sql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB
var models []interface{}

func init() {
	models = make([]interface{}, 0)
}

// Open and initialise the DB global variable and run AutoMigrate for all the registered models.
func Open(config Config) error {
	var err error
	if DB, err = gorm.Open(postgres.Open(configToDSN(config)), &gorm.Config{}); err != nil {
		return err
	}
	if err = DB.AutoMigrate(models...); err != nil {
		return err
	}
	return nil
}

// Close the DB connection and set the DB variable to nil.
func Close() {
	var sqlDB *sql.DB
	var err error
	if sqlDB, err = DB.DB(); err != nil {
		panic(err)
	}
	if err = sqlDB.Close(); err != nil {
		panic(err)
	}
	DB = nil
}

// RegisterModel will add the given model to the models global variable that will be passed to AutoMigrate when the DB
// connection is opened.
func RegisterModel(model interface{}) {
	models = append(models, model)
}
