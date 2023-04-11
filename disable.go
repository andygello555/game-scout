package main

import (
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"strings"
	"time"
)

// DisablePhase will run the "disable" phase of the Scout procedure. This phase disables the developers who aren't doing
// so well. The number of developers to leave enabled depends on the number of tweets we get to use in the update phase
// the next time around, as well as the number of Reddit developers we want to keep active.
func DisablePhase(state *ScoutState) (err error) {
	// UPDATE developers
	// SET disabled = true
	// WHERE id IN (
	//     SELECT developers.id
	//     FROM (
	//        SELECT developer_snapshots.developer_id, max(version) AS latest_version
	//        FROM developer_snapshots
	//        GROUP BY developer_snapshots.developer_id
	//     ) ds1
	//     JOIN developer_snapshots ds2 ON ds1.developer_id = ds2.developer_id AND ds1.latest_version = ds2.version
	//     JOIN developers ON ds2.developer_id = developers.id
	//     WHERE NOT developers.disabled AND developers.type = <DEVELOPER TYPE>
	//     ORDER BY ds2.weighted_score
	//     LIMIT <TOTAL NON DISABLED DEVELOPERS> - maxNonDisabledDevelopers
	// );
	scoutResultAny, _ := state.GetCachedField(StateType).Get("Result")
	if err = scoutResultAny.(*models.ScoutResult).DisableStats.Before(db.DB); err != nil {
		log.ERROR.Printf("Could not set Before fields for ScoutResult.DisableStats: %v", err)
		err = nil
	}

	// Create a reusable query to fetch us how many enabled developers we have currently
	enabledDevelopersQuery := db.DB.Model(&models.Developer{}).Where("NOT disabled").Session(&gorm.Session{})
	totalDisabledDevelopers := 0
	start := time.Now().UTC()

	// Iterate over each Developer Type and disable the required amount for each
	for _, devType := range models.UnknownDeveloperType.Types() {
		var enabledDevelopers int64
		if err = enabledDevelopersQuery.Where("type = ?", devType).Count(&enabledDevelopers).Error; err == nil {
			log.INFO.Printf("There are %d enabled %s developers", enabledDevelopers, devType.String())
			if maxEnabled := int64(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterDisablePhase)); enabledDevelopers > maxEnabled {
				limit := enabledDevelopers - maxEnabled
				log.INFO.Printf("Disabling %d of the worst performing %s developers", limit, devType)
				var disabled []*models.Developer
				if update := db.DB.Model(&disabled).Clauses(clause.Returning{}).Where(
					"id IN (?)",
					db.DB.Table(
						"(?) AS ds1",
						db.DB.Model(
							&models.DeveloperSnapshot{},
						).Select(
							"developer_snapshots.developer_id, max(version) AS latest_version",
						).Group("developer_snapshots.developer_id"),
					).Select(
						"developers.id",
					).Joins(
						"JOIN developer_snapshots ds2 ON ds1.developer_id = ds2.developer_id AND ds1.latest_version = ds2.version",
					).Joins(
						"JOIN developers on ds2.developer_id = developers.id",
					).Where(
						"NOT developers.disabled AND developers.type = ?",
						devType,
					).Order(
						"ds2.weighted_score",
					).Limit(int(limit)),
				).Update(
					"disabled", true,
				); update.Error != nil {
					log.ERROR.Printf(
						"Could not update %d %s developers to disabled status: %v",
						limit, devType.String(), update.Error.Error(),
					)
					return errors.Wrapf(
						update.Error, "could not update %d %s developers to disabled status",
						limit, devType.String(),
					)
				}

				// Add the IDs of all the disabled Developers to the DisabledDevelopers field
				state.GetCachedField(StateType).SetOrAdd("DisabledDevelopers", slices.Comprehension(
					disabled,
					func(idx int, value *models.Developer, arr []*models.Developer) models.DeveloperMinimal {
						return models.DeveloperMinimal{
							ID:   value.ID,
							Type: value.Type,
						}
					},
				))
				log.INFO.Printf("Successfully disabled %d %s developers", limit, devType.String())
				totalDisabledDevelopers += len(disabled)
			} else {
				log.WARNING.Printf(
					"Because there are only %d enabled %s developers, there is no need to disable any %s developers "+
						"(max enabled developers = %d)",
					enabledDevelopers, devType.String(), devType.String(), maxEnabled,
				)
			}
		} else {
			log.ERROR.Printf(
				"Could not count the number of disabled %s Developers: %v. Skipping Disable phase...",
				devType.String(), err.Error(),
			)
		}
	}
	log.INFO.Printf(
		"Disabled %d %s Developers in %s",
		totalDisabledDevelopers, strings.Join(slices.Comprehension(models.UnknownDeveloperType.Types(), func(idx int, value models.DeveloperType, arr []models.DeveloperType) string {
			return value.String()
		}), " and "), time.Now().UTC().Sub(start).String(),
	)

	if err = scoutResultAny.(*models.ScoutResult).DisableStats.After(db.DB); err != nil {
		log.ERROR.Printf("Could not set After fields for ScoutResult.DisableStats: %v", err)
		err = nil
	}
	return
}
