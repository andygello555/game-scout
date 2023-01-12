package db

import (
	"database/sql"
	"database/sql/driver"
	"flag"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	myModels "github.com/andygello555/game-scout/db/models"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/pkg/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"
)

var DB *gorm.DB
var models map[string]*DBModel
var enums []Enum
var extensions []string
var c Config

type DBModel struct {
	Schema *schema.Schema
	Model  any
}

// RW returns a copy of DB with the appropriate gorm.Session and hooks applied so that the read/write permissions of the
// permission of the given ID will be reflected in any queries run using the returned gorm.DB. This is achieved by
// setting the DryRun flag in the gorm.Session/gorm.DB appropriately.
func RW(permissionID int) (db *gorm.DB) {
	read, write := c.DBRWAccessForID(permissionID)
	db = DB
	if !read {
		db = DB.Session(&gorm.Session{DryRun: true})
	} else if !write {
		db = DB.Session(&gorm.Session{})
		writeBlock := func(db *gorm.DB) { db.DryRun = true }
		if err := myErrors.MergeErrors(
			db.Callback().Delete().After("gorm:before_delete").Register("write_block:delete", writeBlock),
			db.Callback().Update().After("gorm:before_update").Register("write_block:update", writeBlock),
			db.Callback().Create().After("gorm:before_delete").Register("write_block:create", writeBlock),
			db.Callback().Raw().Before("gorm:raw").Register("write_block:raw", writeBlock),
		); err != nil {
			panic(err)
		}
	}
	return
}

// ColumnDBNames gets all the column names of the DBModel that are not the empty string.
func (dbm *DBModel) ColumnDBNames() []string {
	columnNames := make([]string, 0)
	for _, field := range dbm.Schema.Fields {
		if field.DBName != "" {
			columnNames = append(columnNames, field.DBName)
		}
	}
	return columnNames
}

// ColumnDBNamesExcluding gets all the column names of the DBModel that are not the empty string excluding the names
// given.
func (dbm *DBModel) ColumnDBNamesExcluding(names ...string) []string {
	columnNames := make([]string, 0)
	for _, field := range dbm.Schema.Fields {
		if field.DBName != "" {
			exclude := false
			for _, name := range names {
				if field.DBName == name {
					exclude = true
					break
				}
			}
			if !exclude {
				columnNames = append(columnNames, field.DBName)
			}
		}
	}
	return columnNames
}

func init() {
	models = make(map[string]*DBModel, 0)
	enums = make([]Enum, 0)
	extensions = make([]string, 0)
	RegisterModel(&myModels.Developer{})
	RegisterModel(&myModels.DeveloperSnapshot{})
	RegisterModel(&myModels.Game{})
	RegisterModel(&myModels.SteamApp{})
	RegisterEnum(myModels.UnknownStorefront)
	RegisterExtension("uuid-ossp")
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

// ComputedFieldsModel represents a model that has computed fields that should be updated each time an instance of that
// model is created or updated. This does not cover fields that should only be computed solely on create or update.
type ComputedFieldsModel interface {
	// UpdateComputedFields updates the computed fields for the instance of the ComputedFieldsModel.
	UpdateComputedFields(tx *gorm.DB) (err error)
	// Empty returns a pointer to an empty instance of the ComputedFieldsModel that can be used as the output for
	// gorm.DB.ScanRows for instance.
	Empty() any
	// Order returns the SQL expression that will be used for ordering the entire result set. This is used in
	// UpdateComputedFieldsForModels.
	Order() string
}

type Upsertable interface {
	OnConflict() clause.OnConflict
	OnCreateOmit() []string
}

// Open and initialise the DB global variable and run AutoMigrate for all the registered models.
func Open(config Config) error {
	var err error
	c = config
	dsn := configToDSN(config)
	dbName := config.DBName()
	if flag.Lookup("test.v") != nil {
		dsn = configToTestDSN(config)
		dbName = config.TestDBName()
		log.INFO.Printf("In testing mode. Creating new database: %s (%s)", dbName, dsn)
	}
	if err = createDB(dbName, config); err != nil {
		return err
	}
	if DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		//Logger: logger.Default.LogMode(logger.Info),
	}); err != nil {
		return err
	}
	createEnums()
	createExtensions()

	// Migrate the models
	var i int
	migratedModels := make([]any, len(models))
	for _, model := range models {
		migratedModels[i] = model.Model
		i++
	}
	if err = DB.AutoMigrate(migratedModels...); err != nil {
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

// UpdateComputedFieldsForModels will update the computed fields in all rows of the given model names.
func UpdateComputedFieldsForModels(modelNames []string, pks []any) (err error) {
	log.INFO.Printf("Running UpdateComputedFieldsForModels")
	if len(modelNames) == 0 {
		for modelName := range models {
			modelNames = append(modelNames, modelName)
		}
	}

	// We defer a function to recover from any panics that occur in the goroutines for each page and set the err return
	// parameter.
	defer func() {
		if pan := recover(); pan != nil {
			switch pan.(type) {
			case error:
				err = errors.Wrap(pan.(error), "could not update computed model instances")
			default:
				panic(pan)
			}
		}
	}()

	const (
		pageSize    = 100
		pageWorkers = 20
	)

	for i, modelName := range modelNames {
		model := models[modelName]
		pkFieldName := model.Schema.PrimaryFieldDBNames[0]
		if _, ok := model.Model.(ComputedFieldsModel); ok {
			var count int64
			countQuery := DB.Model(model.Model)
			if len(pks) > 0 {
				countQuery = countQuery.Where(fmt.Sprintf("%s IN ?", pkFieldName), pks)
			}
			if err = countQuery.Count(&count).Error; err != nil {
				return errors.Wrapf(err, "could not find count for Model %s", modelName)
			}
			if count > 0 {
				log.INFO.Printf("\t%d: Updating computed fields for %s, %d rows", i+1, modelName, count)
				pages := int(math.Ceil(float64(count) / float64(pageSize)))

				var wg sync.WaitGroup
				jobs := make(chan int, pages)

				// Start workers that will update pages of the current model
				for w := 0; w < pageWorkers; w++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for job := range jobs {
							var workerErr error
							var rows *sql.Rows

							// We find the rows with the limit and the offset for the page
							pageQuery := DB.Model(
								model.Model,
							).Order(
								model.Model.(ComputedFieldsModel).Order(),
							).Limit(
								pageSize,
							).Offset(
								job * pageSize,
							)

							if len(pks) > 0 {
								pageQuery = pageQuery.Where(fmt.Sprintf("%s IN ?", pkFieldName), pks)
							}

							if rows, workerErr = pageQuery.Rows(); workerErr != nil {
								panic(workerErr)
							}

							rowsProcessed := 0
							start := time.Now()
							for rows.Next() {
								// For each row we will scan the row into an empty instance of the ComputedFieldsModel.
								instance := model.Model.(ComputedFieldsModel).Empty()
								if workerErr = DB.ScanRows(rows, instance); workerErr != nil {
									panic(workerErr)
								}

								//beforeScore := instance.(*myModels.DeveloperSnapshot).WeightedScore
								// We then assert the instance to a ComputedFieldsModel and call the UpdateComputedFields
								// method.
								if workerErr = instance.(ComputedFieldsModel).UpdateComputedFields(DB); workerErr != nil {
									panic(workerErr)
								}

								//afterScore := instance.(*myModels.DeveloperSnapshot).WeightedScore
								//if afterScore != beforeScore {
								//	log.WARNING.Printf("%f != %f", beforeScore, afterScore)
								//}
								// Finally, save the instance
								if workerErr = DB.Save(instance).Error; workerErr != nil {
									panic(workerErr)
								}
								rowsProcessed++
							}

							log.INFO.Printf(
								"Processed %d rows from %d to %d in %s",
								rowsProcessed, job*pageSize, job*pageSize+pageSize, time.Now().Sub(start).String(),
							)
							if workerErr = rows.Close(); workerErr != nil {
								panic(workerErr)
							}
						}
					}()
				}

				// Queue up all the pages to the workers
				for page := 0; page < pages; page++ {
					jobs <- page
				}

				// Wait for each goroutine for each page to finish
				close(jobs)
				wg.Wait()
			} else {
				log.INFO.Printf("\t%d: %s has no rows. Skipping", i+1, modelName)
			}
		} else {
			log.INFO.Printf("\t%d: %s is not a ComputedFieldsModel. Skipping...", i+1, modelName)
		}
	}
	return
}

// Upsert will upsert the given Upsertable value in the current DB.
func Upsert(value Upsertable) (created bool, err error) {
	created = false
	// First we do a null check
	var updateOrCreate *gorm.DB
	if value == nil || (reflect.ValueOf(value).Kind() == reflect.Ptr && reflect.ValueOf(value).IsNil()) {
		return
	}

	// Then we construct the upsert clause
	onConflict := value.OnConflict()
	if len(onConflict.DoUpdates) == 0 {
		onConflict.DoUpdates = clause.AssignmentColumns(GetModel(value).ColumnDBNamesExcluding("id"))
	}
	if updateOrCreate = DB.Clauses(onConflict); updateOrCreate.Error != nil {
		err = myErrors.TemporaryWrapf(
			false,
			updateOrCreate.Error,
			"could not create update or create clause for %s",
			reflect.TypeOf(value).Elem().Name(),
		)
		return
	}

	// Finally, we create the model instance, omitting any fields that should be omitted
	if after := updateOrCreate.Omit(value.OnCreateOmit()...).Create(value); after.Error != nil {
		err = errors.Wrapf(after.Error, "could not upsert %s", reflect.TypeOf(value).Elem().Name())
		return
	}
	log.INFO.Printf("Upserted %s", reflect.TypeOf(value).Elem().Name())
	created = true
	return
}

// RegisterModel will find the schema of the given model, wrap this information in a DBModel and add it to the models
// global mapping that will be passed to AutoMigrate when the DB connection is opened.
func RegisterModel(model any) {
	if s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{}); err != nil {
		log.ERROR.Printf("Could not register model %s, could not find schema", reflect.TypeOf(model).String())
		panic(err)
	} else {
		log.INFO.Printf("Registering model: %s", reflect.TypeOf(model).Elem().Name())
		models[reflect.TypeOf(model).Elem().Name()] = &DBModel{
			Schema: s,
			Model:  model,
		}
	}
}

// RegisterEnum will add the given enum to the enums global variable. The enums in this variable will be created at
// startup by createEnums.
func RegisterEnum(enum Enum) {
	enums = append(enums, enum)
}

// RegisterExtension will add the given extension name to the extensions global variable. The extensions in this
// variable will be created at startup by createExtensions.
func RegisterExtension(extension string) {
	extensions = append(extensions, extension)
}

// GetModel will return the DBModel from the models mapping that matches the type name of the given model.
func GetModel(model any) *DBModel {
	return models[reflect.TypeOf(model).Elem().Name()]
}

// connectPostgres connects to the postgres DB via GORM. It returns the gorm.DB as well as a function to close the
// connection
func connectPostgres(config Config) (db *gorm.DB, close func(), err error) {
	if db, err = gorm.Open(postgres.Open(configToPostgresDSN(config)), &gorm.Config{}); err != nil {
		return
	}
	return db, func() {
		var sqlDB *sql.DB
		if sqlDB, err = db.DB(); err != nil {
			return
		}
		err = sqlDB.Close()
	}, err
}

// createDB will create the DB if it doesn't exist.
func createDB(dbName string, config Config) (err error) {
	// We connect temporarily to the postgres DB so that we can create the DB
	var (
		db *gorm.DB
		c  func()
	)
	if db, c, err = connectPostgres(config); err != nil {
		return err
	}
	defer c()

	result := db.Exec(fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = '%s';", dbName))
	switch {
	case result.RowsAffected == 0:
		if err = db.Exec(fmt.Sprintf("CREATE DATABASE %s;", dbName)).Error; err != nil {
			log.ERROR.Printf("Cannot create DB %s: %s", dbName, err.Error())
			return err
		}
	case result.Error != nil:
		return result.Error
	}
	return
}

// DropDB deletes the DB with the given name if it exists.
func DropDB(dbName string, config Config) (err error) {
	// We connect temporarily to the postgres DB so that we can create the DB
	var (
		db *gorm.DB
		c  func()
	)
	if db, c, err = connectPostgres(config); err != nil {
		return err
	}
	defer c()

	result := db.Exec(fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = '%s';", dbName))
	switch {
	case result.RowsAffected == 1:
		if err = db.Exec(fmt.Sprintf("DROP DATABASE %s;", dbName)).Error; err != nil {
			log.ERROR.Printf("Cannot drop DB %s: %s", dbName, err.Error())
			return err
		}
	case result.Error != nil:
		return result.Error
	}
	return
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
				log.ERROR.Printf("Cannot create %s enum type from %s.%s", enum.Type(), reflect.TypeOf(enum).String(), enum.String())
				panic(err)
			}
		case result.Error != nil:
			panic(result.Error)
		}
	}
}

// createExtensions will create all the extensions in the DB.
func createExtensions() {
	for _, extension := range extensions {
		if err := DB.Exec(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS \"%s\";", extension)).Error; err != nil {
			log.ERROR.Printf("Cannot create extension %s for DB: %s", extension, err.Error())
			panic(err)
		}
	}
}
