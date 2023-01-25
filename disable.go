package main

import (
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/pkg/errors"
	"gorm.io/gorm/clause"
	"math"
)

// DisablePhase will run the "disable" phase of the Scout procedure. This phase disables the developers who aren't doing
// so well. The number of developers to leave enabled depends on the number of tweets we get to use in the update phase
// the next time around.
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
	//     WHERE NOT developers.disabled
	//     ORDER BY ds2.weighted_score
	//     LIMIT <TOTAL NON DISABLED DEVELOPERS> - maxNonDisabledDevelopers
	// );
	scoutResultAny, _ := state.GetCachedField(StateType).Get("Result")
	if err = scoutResultAny.(*models.ScoutResult).DisableStats.Before(db.DB); err != nil {
		log.ERROR.Printf("Could not set Before fields for ScoutResult.DisableStats: %v", err)
		err = nil
	}

	var enabledDeveloperCount int64
	if err = db.DB.Model(&models.Developer{}).Where("NOT disabled").Count(&enabledDeveloperCount).Error; err == nil {
		log.INFO.Printf("There are %d enabled developers", enabledDeveloperCount)
		if maxEnabled := int64(math.Floor(maxEnabledDevelopersAfterDisablePhase)); enabledDeveloperCount > maxEnabled {
			limit := enabledDeveloperCount - maxEnabled
			log.INFO.Printf("Disabling %d of the worst performing developers", limit)
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
					"NOT developers.disabled",
				).Order(
					"ds2.weighted_score",
				).Limit(int(limit)),
			).Update(
				"disabled", true,
			); update.Error != nil {
				log.ERROR.Printf("Could not update %d developers to disabled status: %v", limit, update.Error.Error())
				return errors.Wrapf(update.Error, "could not update %d developers to disabled status", limit)
			}

			// Add the IDs of all the disabled Developers to the DisabledDevelopers field
			state.GetCachedField(StateType).SetOrAdd("DisabledDevelopers", slices.Comprehension(
				disabled,
				func(idx int, value *models.Developer, arr []*models.Developer) string { return value.ID },
			))
			log.INFO.Printf("Successfully disabled %d developers", limit)
		} else {
			log.WARNING.Printf(
				"Because there are only %d enabled developers, there is no need to disable any developers (max "+
					"enabled developers = %d)",
				enabledDeveloperCount, maxEnabled,
			)
		}
	} else {
		log.ERROR.Printf("Could not count the number of disabled Developers: %v. Skipping Disable phase...", err.Error())
	}

	if err = scoutResultAny.(*models.ScoutResult).DisableStats.After(db.DB); err != nil {
		log.ERROR.Printf("Could not set After fields for ScoutResult.DisableStats: %v", err)
		err = nil
	}
	return
}
