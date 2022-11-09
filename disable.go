package main

import (
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/pkg/errors"
	"math"
)

// DisablePhase will run the "disable" phase of the Scout procedure. This phase disables the developers who aren't doing
// so well. The number of developers to disable depends on the number of tweets we get to use in the update phase the
// next time around.
func DisablePhase() (err error) {
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
	//     LIMIT <TOTAL NON DISABLED DEVELOPERS - maxNonDisabledDevelopers
	// );

	var enabledDeveloperCount int64
	if err = db.DB.Model(&models.Developer{}).Where("NOT disabled").Count(&enabledDeveloperCount).Error; err == nil {
		log.INFO.Printf("There are %d enabled developers", enabledDeveloperCount)
		if maxEnabled := int64(math.Floor(maxEnabledDevelopers)); enabledDeveloperCount > maxEnabled {
			limit := enabledDeveloperCount - maxEnabled
			log.INFO.Printf("Disabling %d of the worst performing developers", limit)
			if update := db.DB.Exec(`
				UPDATE developers
				SET disabled = true
				WHERE id IN (
					SELECT developers.id
					FROM (
				   		SELECT developer_snapshots.developer_id, max(version) AS latest_version
						FROM developer_snapshots
	   					GROUP BY developer_snapshots.developer_id
					) ds1
					JOIN developer_snapshots ds2 ON ds1.developer_id = ds2.developer_id AND ds1.latest_version = ds2.version
					JOIN developers ON ds2.developer_id = developers.id
					WHERE NOT developers.disabled
					ORDER BY ds2.weighted_score
					LIMIT ?
				);
			`, limit); update.Error != nil {
				log.ERROR.Printf("Could not update %d developers to disabled status: %v", limit, update.Error.Error())
				return errors.Wrapf(update.Error, "could not update %d developers to disabled status", limit)
			}
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
	return
}
