package db

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	myModels "github.com/andygello555/game-scout/db/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"reflect"
	"strings"
)

var DB *gorm.DB
var models []any
var enums []Enum

func init() {
	models = make([]any, 0)
	enums = make([]Enum, 0)
	RegisterModel(&myModels.Developer{})
	RegisterModel(&myModels.DeveloperSnapshot{})
	RegisterModel(&myModels.Game{})
	RegisterEnum(myModels.UnknownStorefront)
}

// Enum represents an enum type that should be created on migration
type Enum interface {
	fmt.Stringer
	driver.Valuer
	// Type returns the name of the type that should be created in the DB.
	Type() string
	// Values returns the possible values of the enum.
	Values() []string
}

// Open and initialise the DB global variable and run AutoMigrate for all the registered models.
func Open(config Config) error {
	var err error
	if DB, err = gorm.Open(postgres.Open(configToDSN(config)), &gorm.Config{}); err != nil {
		return err
	}
	createEnums()
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
func RegisterModel(model any) {
	models = append(models, model)
}

// RegisterEnum will add the given enum to the enums global variable. The enums in this variable will be created at
// startup by createEnums.
func RegisterEnum(enum Enum) {
	enums = append(enums, enum)
}

// createEnums will create all the enums as types in the DB.
func createEnums() {
	for _, enum := range enums {
		result := DB.Exec(fmt.Sprintf("SELECT 1 FROM pg_type WHERE typname = '%s';", enum.Type()))
		switch {
		case result.RowsAffected == 0:
			enumValues := enum.Values()
			for i, value := range enumValues {
				enumValues[i] = fmt.Sprintf("'%s'", value)
			}
			if err := DB.Exec(fmt.Sprintf(
				"CREATE TYPE %s AS ENUM (%s);",
				enum.Type(),
				strings.Join(enumValues, ", "),
			)).Error; err != nil {
				log.ERROR.Printf("Cannot create %s enum type from %s.%s", enum.Type(), reflect.TypeOf(enum), enum.String())
				panic(err)
			}
		case result.Error != nil:
			panic(result.Error)
		}
	}
}
