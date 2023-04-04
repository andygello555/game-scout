package main

import (
	"container/heap"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/game-scout/email"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/andygello555/gotils/v2/numbers"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"math"
	"strings"
	"time"
)

// DeletePhase will run the Disable phase of the Scout procedure. It is only run on the same days that the Measure Phase
// is run. This day can be changed in the TemplateConfig.SendDay in the config.json.
//
// This Phase first deletes any developers that have less than 3 snapshots and their latest snapshot was created over
// staleDeveloperDays ago.
//
// Then the Phase will delete the lowest performing disabled developers. We find the lowest performing developers by
// finding the trend for each, and sorting it by the value of the first coefficient for each regression in ascending order.
// The top percentageOfDisabledDevelopersToDelete of this set will be deleted.
//
// All games that reference these deleted developers will have the developer's username removed from its "developers"
// field. If any unwatched games after this procedure are left with no developers within their developers field, they also
// will be deleted.
//
// The deleted developers are stored within the DeletedDevelopers cached field of the given ScoutState, along with all
// their snapshots and games, so that they can be mentioned in the Measure Phase.
func DeletePhase(state *ScoutState) (err error) {
	startAny, _ := state.GetCachedField(StateType).Get("Start")
	start := startAny.(time.Time)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	debugAny, _ := state.GetCachedField(StateType).Get("Debug")
	debug := debugAny.(bool)

	// If the weekday of the start of the scrape does not match the weekday that Measure emails are sent out on and
	// debug is not set, then the DeletePhase does not need to be run.
	if !debug && globalConfig.Email.EmailTemplateConfigFor(email.Measure).TemplateSendDay() != start.Weekday() {
		log.WARNING.Printf(
			"Delete Phase does not need to be run as Scout was started on %s, which was/is not a %s",
			start.Format("Monday 2 January"),
			globalConfig.Email.EmailTemplateConfigFor(email.Measure).TemplateSendDay().String(),
		)
		return nil
	}

	scoutResultAny, _ := state.GetCachedField(StateType).Get("Result")
	if err = scoutResultAny.(*models.ScoutResult).DeleteStats.Before(db.DB); err != nil {
		log.ERROR.Printf("Could not set Before fields for ScoutResult.DeleteStats: %v", err)
		err = nil
	}

	// First we find the total number of disabled developers and how many of those need to be deleted
	var totalDisabledDevelopers int64
	if err = db.DB.Model(&models.Developer{}).Where("disabled").Count(&totalDisabledDevelopers).Error; err != nil {
		return myErrors.TemporaryWrap(false, err, "could not find the total number of disabled developers")
	}
	developersToDelete := int(math.Floor(float64(totalDisabledDevelopers) * globalConfig.Scrape.Constants.PercentageOfDisabledDevelopersToDelete))

	// We also set the developersToGo variable so that it won't be skewed by the number of stale developers. This will
	// also subtract the number of existing deleted developers in the cache.
	// NOTE: Because the deleted developers also stores the stale developers, if a previous scrape exits after finding
	//       the stale developers, the developersToGo of the next run will be the developersToDelete minus the number of
	//       staleDevelopers found in the previous run.
	developersToGo := developersToDelete
	if state.GetIterableCachedField(DeletedDevelopersType).Len() > 0 {
		developersToGo -= state.GetIterableCachedField(DeletedDevelopersType).Len()
		log.WARNING.Printf(
			"ScoutState contains a non-empty DeletedDevelopers list. There are %d DeletedDevelopers already, so "+
				"there are %d developers still left to delete...",
			state.GetIterableCachedField(DeletedDevelopersType).Len(), developersToGo,
		)
	}

	// We only select developers that have 0 verified games
	whereZeroVerified := db.DB.Model(&models.Game{}).Select(
		"COUNT(*)",
	).Where(
		"developers.type || developers.username = ANY(games.verified_developer_usernames)",
	)

	latestSnapshotQuery := db.DB.Model(
		&models.DeveloperSnapshot{},
	).Select(
		"developer_snapshots.developer_id, max(version) AS latest_version",
	).Group("developer_snapshots.developer_id")

	// First find any developers that haven't had a snapshot for a while and add them to the developers to be deleted
	log.INFO.Printf(
		"First we are going to queue up any developers that haven't had a new snapshot in %d days to be deleted",
		globalConfig.Scrape.Constants.StaleDeveloperDays,
	)

	// SELECT developers.*
	// FROM (
	//   SELECT developer_snapshots.developer_id, max(version) AS latest_version
	//   FROM developer_snapshots
	//   GROUP BY developer_snapshots.developer_id
	// ) AS ds1
	// JOIN developer_snapshots ds2 ON ds1.developer_id = ds2.developer_id AND ds1.latest_version = ds2.version
	// JOIN developers ON ds2.developer_id = developers.id
	// WHERE ds2.created_at < NOW() - (<staleDeveloperDays> * INTERVAL '1 DAY') AND ds2.version < 2 AND (
	//   SELECT COUNT(*)
	//   FROM games
	//   WHERE developers.type || developers.username = ANY(games.verified_developer_usernames)
	// ) = 0
	// ORDER BY ds2.weighted_score

	query := db.DB.Table(
		"(?) AS ds1",
		latestSnapshotQuery,
	).Select(
		"developers.*",
	).Joins(
		"JOIN developer_snapshots ds2 ON ds1.developer_id = ds2.developer_id AND ds1.latest_version = ds2.version",
	).Joins(
		"JOIN developers ON ds2.developer_id = developers.id",
	).Where(
		"ds2.created_at < NOW() - (? * INTERVAL '1 DAY') AND ds2.version < 2 AND (?) = 0",
		globalConfig.Scrape.Constants.StaleDeveloperDays,
		whereZeroVerified,
	).Order(
		"ds2.created_at",
	)

	var staleDevelopers []*models.Developer
	if err = query.Find(&staleDevelopers).Error; err != nil {
		return myErrors.TemporaryWrap(false, err, "could not find stale developers in Delete Phase")
	}
	log.INFO.Printf(
		"Found %d stale developers. Creating TrendingDevs for each and adding them to the DeletedDevelopers cached field",
		len(staleDevelopers),
	)

	added := 0
	for _, staleDeveloper := range staleDevelopers {
		var trendingDev *models.TrendingDev
		if trendingDev, err = staleDeveloper.TrendingDev(db.DB); err != nil && !strings.Contains(err.Error(), "could not find Trend for") {
			log.WARNING.Printf("Error occurred whilst creating TrendingDev for stale developer: %v", err)
			continue
		}
		state.GetIterableCachedField(DeletedDevelopersType).SetOrAdd(trendingDev)
		added++
		log.INFO.Printf("\tAdded stale Developer %v to DeletedDevelopers", staleDeveloper)
	}
	log.INFO.Printf("Finished adding %d/%d stale Developers to DeletedDevelopers", added, len(staleDevelopers))

	log.INFO.Printf(
		"There are %d total disabled developers in the DB, we have to delete %.1f%% of those, which is %d",
		totalDisabledDevelopers, globalConfig.Scrape.Constants.PercentageOfDisabledDevelopersToDelete*100, developersToDelete,
	)

	// SELECT developers.*
	// FROM (
	//   SELECT developer_snapshots.developer_id, max(version) AS latest_version
	//   FROM developer_snapshots
	//   GROUP BY developer_snapshots.developer_id
	// ) AS ds1
	// JOIN developer_snapshots ds2 ON ds1.developer_id = ds2.developer_id AND ds1.latest_version = ds2.version
	// JOIN developers ON ds2.developer_id = developers.id
	// WHERE developers.disabled AND (
	//   SELECT COUNT(*)
	//   FROM games
	//   WHERE developers.type || developers.username = ANY(games.verified_developer_usernames)
	// ) = 0
	// ORDER BY ds2.weighted_score

	query = db.DB.Table(
		"(?) AS ds1",
		latestSnapshotQuery,
	).Select(
		"developers.*",
	).Joins(
		"JOIN developer_snapshots ds2 ON ds1.developer_id = ds2.developer_id AND ds1.latest_version = ds2.version",
	).Joins(
		"JOIN developers ON ds2.developer_id = developers.id",
	).Where(
		"developers.disabled AND (?) = 0",
		whereZeroVerified,
	).Order(
		"ds2.weighted_score",
	)

	var totalCount int64
	if err = query.Select("*").Count(&totalCount).Error; err != nil {
		log.ERROR.Printf(
			"Could not determine the total count of all developers in the main query for the Delete phase: %v",
			err,
		)
	} else {
		log.INFO.Printf("Total disabled developers to sample from: %d", totalCount)
	}

	offset := 0
	sample := 0
	for developersToGo > 0 {
		log.INFO.Printf(
			"There are %d developers to find that should be deleted. We have found %d developers to delete. "+
				"Limit = %d, Offset = %d, Sample no. = %d",
			developersToGo, state.GetIterableCachedField(DeletedDevelopersType).Len(), developersToGo*2, offset, sample+1,
		)

		var developerSample []*models.Developer
		if err = query.Limit(developersToGo * 2).Offset(offset).Find(&developerSample).Error; err != nil {
			return errors.Wrapf(
				err,
				"could not fetch sample of %d developers to find out whether we should delete them",
				developersToGo*2,
			)
		}
		log.INFO.Printf("We have found %d developers to sample for sample no. %d", len(developerSample), sample+1)

		if len(developerSample) == 0 {
			log.WARNING.Printf(
				"We have run out of developers to sample to be deleted. There are still %d developers to go...",
				developersToGo,
			)
			break
		}

		log.INFO.Printf("Finding the games, snapshots, and trends of %d developers...", len(developerSample))

		// Offset becomes the old limit
		offset += developersToGo * 2

		// Fetch the Games, Snapshots, and Trend for each developer and push the results to a trendFinderResultHeap
		developerSortedSample := newTrendFinderResultHeap(true)
		for _, developer := range developerSample {
			if _, ok := state.GetIterableCachedField(DeletedDevelopersType).Get(developer.ID); !ok {
				result := &trendFinderResult{}
				if result.TrendingDev, result.err = developer.TrendingDev(db.DB); result.err != nil {
					log.WARNING.Printf("Error whilst creating TrendingDev: %v, skipping...", result.err)
					continue
				}

				heap.Push(&developerSortedSample, result)

				// We know that any developer added to the heap has successfully been fetched, so we break out of this
				// loop if we have already reached the total remaining developers that we need. The only time this check
				// should never succeed is when the heap contains less than developersToGo developers that can
				// successfully call .Trend.
				if developerSortedSample.Len() >= developersToGo {
					log.WARNING.Printf(
						"We already have found %d/%d trend-ful developers in the sample, so we are going to stop finding "+
							"trends for this sample.",
						developerSortedSample.Len(), developersToGo,
					)
					break
				}
				state.GetCachedField(StateType).SetOrAdd("Result", "DeleteStats", "TotalSampledDevelopers", models.SetOrAddInc.Func())
			} else {
				// This should never happen, as the sample is sorted by weighted_score and offset and limit to create
				// distinct batches.
				log.WARNING.Printf("DeletedDevelopers already contains ID for Developer %v", developer)
			}
		}

		// Find the number of developers to pop from the heap. This is the minimum of the length of the heap and the
		// developers remaining.
		developersNeeded := numbers.Min(developerSortedSample.Len(), developersToGo)
		log.INFO.Printf(
			"a = developerSortedSample.Len = %d, b = developersToGo = %d, min(a, b) = %d",
			developerSortedSample.Len(), developersToGo, developersNeeded,
		)
		developersToGo -= developersNeeded
		for i := 0; i < developersNeeded; i++ {
			developer := heap.Pop(&developerSortedSample).(*trendFinderResult)
			log.INFO.Printf(
				"Developer %v is placed %s out of %d developers in sample no. %d with coeff[1] = %.10f",
				developer.Developer, numbers.Ordinal(i+1), developersNeeded, sample+1, developer.Trend.GetCoeffs()[1],
			)
			state.GetIterableCachedField(DeletedDevelopersType).SetOrAdd(developer.TrendingDev)
		}
		if err = state.Save(); err != nil {
			log.ERROR.Printf("Could not save State cache to disk in Enable: %v", err)
		}
		state.GetCachedField(StateType).SetOrAdd("Result", "DeleteStats", "TotalFinishedSamples", models.SetOrAddInc.Func())
		sample++
	}

	// Finally, delete all the developers in the DeletedDevelopers iterable cached field
	deletedDevelopers := state.GetIterableCachedField(DeletedDevelopersType).(*DeletedDevelopers)
	actuallyDeletedDevs := 0
	log.INFO.Printf("Deleting %d under performing developers", deletedDevelopers.Len())
	if deletedDevelopers.Len() > 0 {
		// Iterate over the deleted developers and remove their reference from each related Game. If the Game has no
		// developers left after this modification, then we will delete that Game.
		iter := deletedDevelopers.Iter()
		for iter.Continue() {
			deletedDev := iter.Key().(*models.TrendingDev)
			log.INFO.Printf(
				"Starting process to remove any traces of Developer %v which is %s in our list of %d devs to delete",
				deletedDev.Developer, numbers.Ordinal(iter.I()+1), deletedDevelopers.Len(),
			)
			gameIDs := slices.Comprehension(deletedDev.Games, func(idx int, value *models.Game, arr []*models.Game) uuid.UUID {
				return value.ID
			})

			// Update the developers games to remove their username from the developers field
			log.INFO.Printf(
				"\tRemoving reference to \"%s\" from the developers field of %d Games for Developer %v",
				deletedDev.Developer.Username, len(gameIDs), deletedDev.Developer,
			)
			if err = db.DB.Model(
				&models.Game{},
			).Where(
				"id IN ?",
				gameIDs,
			).Update(
				"developers",
				gorm.Expr("array_remove(developers, ?)", deletedDev.Developer.TypedUsername()),
			).Error; err != nil {
				log.ERROR.Printf(
					"\tCould not remove \"%s\" from the developers field of all Games related to Developer %v: %v",
					deletedDev.Developer.Username, deletedDev.Developer, err,
				)
			}

			// Delete any Games that now have no referenced developers and are not being watched
			deleteQuery := db.DB.Model(&models.Game{}).Where(
				"id IN ? AND cardinality(developers) = 0 AND watched IS NULL",
				gameIDs,
			)

			var toBeDeletedGames int64
			if err = deleteQuery.Count(&toBeDeletedGames).Error; err != nil {
				log.ERROR.Printf(
					"\tCould not find the number of Games that have an empty developers field that are also related to "+
						"Developer %v: %v",
					deletedDev.Developer, err,
				)
			}
			log.INFO.Printf(
				"\tDeleting %d/%d Games for Developer %v that now have an empty developers field",
				toBeDeletedGames, len(gameIDs), deletedDev.Developer,
			)

			if deleteResult := deleteQuery.Delete(&models.Game{}); deleteResult.Error != nil {
				log.ERROR.Printf(
					"\tCould not delete %d Games for Developer %v that now have an empty developers field: %v",
					toBeDeletedGames, deletedDev.Developer, err,
				)
			} else if toBeDeletedGames > 0 {
				log.INFO.Printf(
					"\tDeleted %d/%d Games for Developer %v that now have empty developers fields",
					deleteResult.RowsAffected, toBeDeletedGames, deletedDev.Developer,
				)
			}

			// Finally, delete this developer
			if err = db.DB.Delete(deletedDev.Developer).Error; err != nil {
				log.ERROR.Printf(
					"Could not delete Developer %v",
					deletedDev.Developer,
				)
			} else {
				log.INFO.Printf("Deleted Developer %v", deletedDev.Developer)
				state.GetCachedField(StateType).SetOrAdd("Result", "DeleteStats", "DeletedDevelopers", models.SetOrAddInc.Func())
				actuallyDeletedDevs++
			}
			iter.Next()
		}
		log.INFO.Printf(
			"Deleted %d/%d disabled developers that should have been deleted",
			actuallyDeletedDevs, deletedDevelopers.Len(),
		)
	} else {
		log.WARNING.Printf("There are no developers to delete...")
	}

	if err = scoutResultAny.(*models.ScoutResult).DeleteStats.After(db.DB); err != nil {
		log.ERROR.Printf("Could not set After fields for ScoutResult.DeleteStats: %v", err)
		err = nil
	}
	return
}
