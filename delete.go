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
)

// DeletePhase will run the "disable" phase of the Scout procedure. This phase deletes the lowest performing disabled
// developers. The way we find the lowest performing developers is by finding the trend for each, and sorting it by the
// value of the first coefficient for each regression in ascending order. The top percentageOfDisabledDevelopersToDelete
// of this set will be deleted.
//
// All games that reference these deleted developers will have the developer's username removed from its developers
// field. If any games after this procedure are left with no developers within their developers field, they also will be
// deleted.
//
// The deleted developers are stored within the DeletedDevelopers cached field of the given ScoutState, along with all
// their snapshots and games, so that they can be mentioned in the Measure Phase.
func DeletePhase(state *ScoutState) (err error) {
	var totalDisabledDevelopers int64
	if err = db.DB.Model(&models.Developer{}).Where("disabled").Count(&totalDisabledDevelopers).Error; err != nil {
		return myErrors.TemporaryWrap(false, err, "could not find the total number of disabled developers")
	}

	developersToDelete := int(math.Floor(float64(totalDisabledDevelopers) * percentageOfDisabledDevelopersToDelete))
	log.INFO.Printf(
		"There are %d total disabled developers in the DB, we have to delete %.2f percent of those, which is %d",
		totalDisabledDevelopers, percentageOfDisabledDevelopersToDelete, developersToDelete,
	)

	query := db.DB.Table(
		"(?) AS ds1",
		db.DB.Model(
			&models.DeveloperSnapshot{},
		).Select(
			"developer_snapshots.developer_id, max(version) AS latest_version",
		).Group("developer_snapshots.developer_id"),
	).Select(
		"developers.*",
	).Joins(
		"JOIN developer_snapshots ds2 ON ds1.developer_id = ds2.developer_id AND ds1.latest_version = ds2.version",
	).Joins(
		"JOIN developers on ds2.developer_id = developers.id",
	).Where(
		"developers.disabled",
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

	developersToGo := developersToDelete
	offset := 0
	sample := 0
	if state.GetIterableCachedField(DeletedDevelopersType).Len() > 0 {
		developersToGo -= state.GetIterableCachedField(DeletedDevelopersType).Len()
		log.WARNING.Printf(
			"ScoutState contains a non-empty DeletedDevelopers list. There are %d DeletedDevelopers already, so "+
				"there are %d developers still left to delete...",
			state.GetIterableCachedField(DeletedDevelopersType).Len(), developersToGo,
		)
	}

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
				result := &trendFinderResult{
					TrendingDev: &email.TrendingDev{
						Developer: developer,
					},
				}

				if result.Games, err = developer.Games(db.DB); err != nil {
					log.WARNING.Printf(
						"Could find games for \"%s\" (%s): %v, skipping...",
						developer.Username, developer.ID, err,
					)
					continue
				}

				if result.Snapshots, err = developer.DeveloperSnapshots(db.DB); err != nil {
					log.WARNING.Printf(
						"Could find snapshots for \"%s\" (%s): %v, skipping...",
						developer.Username, developer.ID, err,
					)
					continue
				}

				if result.Trend, err = developer.Trend(db.DB); err != nil {
					log.WARNING.Printf(
						"Could not find trend for \"%s\" (%s): %v, skipping...",
						developer.Username, developer.ID, err,
					)
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
			} else {
				// This should never happen, as the sample is sorted by weighted_score and offset and limit to create
				// distinct batches.
				log.WARNING.Printf(
					"DeletedDevelopers already contains ID for Developer \"%s\" (%s)",
					developer.Username, developer.ID,
				)
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
				"Developer \"%s\" (%s) is placed %s out of %d developers in sample no. %d with coeff[1] = %.10f",
				developer.Developer.Username, developer.Developer.ID, numbers.Ordinal(i+1), developersNeeded, sample+1,
				developer.Trend.GetCoeffs()[1],
			)
			state.GetIterableCachedField(DeletedDevelopersType).SetOrAdd(developer.TrendingDev)
		}
		if err = state.Save(); err != nil {
			log.ERROR.Printf("Could not save State cache to disk in Enable: %v", err)
		}
		sample++
	}

	// Finally, delete all the developers in the DeletedDevelopers iterable cached field
	deletedDevelopers := state.GetIterableCachedField(DeletedDevelopersType).(*DeletedDevelopers)
	actuallyDeletedDevs := 0
	log.INFO.Printf("Deleting %d under performing developers", deletedDevelopers.Len())
	if debug, _ := state.GetCachedField(StateType).Get("Debug"); !debug.(bool) {
		if deletedDevelopers.Len() > 0 {
			// Iterate over the deleted developers and remove their reference from each related Game. If the Game has no
			// developers left after this modification, then we will delete that Game.
			iter := deletedDevelopers.Iter()
			for iter.Continue() {
				deletedDev := iter.Key().(*email.TrendingDev)
				log.INFO.Printf(
					"Starting process to remove any traces of Developer \"%s\" (%s) which is %s in our list of devs to delete",
					deletedDev.Developer.Username, deletedDev.Developer.ID, numbers.Ordinal(iter.I()+1),
				)
				gameIDs := slices.Comprehension(deletedDev.Games, func(idx int, value *models.Game, arr []*models.Game) uuid.UUID {
					return value.ID
				})

				// Update the developers games to remove their username from the developers field
				log.INFO.Printf(
					"\tRemoving reference to \"%s\" from the developers field of %d Games for Developer \"%s\" (%s)",
					deletedDev.Developer.Username, len(gameIDs), deletedDev.Developer.Username, deletedDev.Developer.ID,
				)
				if err = db.DB.Model(
					&models.Game{},
				).Where(
					"id IN ?",
					gameIDs,
				).Update(
					"developers",
					gorm.Expr("array_remove(developers, ?)", deletedDev.Developer.Username),
				).Error; err != nil {
					log.ERROR.Printf(
						"\tCould not remove \"%s\" from the developers field of all Games related to Developer \"%s\" (%s): %v",
						deletedDev.Developer.Username, deletedDev.Developer.Username, deletedDev.Developer.ID, err,
					)
				}

				// Delete any Games that now have no referenced developers
				deleteQuery := db.DB.Model(&models.Game{}).Where(
					"id IN ? AND cardinality(developers) = 0",
					gameIDs,
				)

				var toBeDeletedGames int64
				if err = deleteQuery.Count(&toBeDeletedGames).Error; err != nil {
					log.ERROR.Printf(
						"\tCould not find the number of Games that have an empty developers field that are also related to "+
							"Developer \"%s\" (%s): %v",
						deletedDev.Developer.Username, deletedDev.Developer.ID, err,
					)
				}
				log.INFO.Printf(
					"\tDeleting %d/%d Games for Developer \"%s\" (%s) that now have an empty developers field",
					toBeDeletedGames, len(gameIDs), deletedDev.Developer.Username, deletedDev.Developer.ID,
				)

				if deleteResult := deleteQuery.Delete(&models.Game{}); deleteResult.Error != nil {
					log.ERROR.Printf(
						"\tCould not delete %d Games for Developer \"%s\" (%s) that now have an empty developers field: %v",
						toBeDeletedGames, deletedDev.Developer.Username, deletedDev.Developer.ID, err,
					)
				} else if toBeDeletedGames > 0 {
					log.INFO.Printf(
						"\tDeleted %d/%d Games for Developer \"%s\" (%s) that now have empty developers fields",
						deleteResult.RowsAffected, toBeDeletedGames, deletedDev.Developer.Username, deletedDev.Developer.ID,
					)
				}

				// Finally, delete this developer
				if err = db.DB.Delete(deletedDev.Developer).Error; err != nil {
					log.ERROR.Printf(
						"Could not delete Developer \"%s\" (%s)",
						deletedDev.Developer.Username, deletedDev.Developer.ID,
					)
				} else {
					log.INFO.Printf("Deleted Developer \"%s\" (%s)", deletedDev.Developer.Username, deletedDev.Developer.ID)
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
	} else {
		log.WARNING.Printf("Skipping deletion of %d developers as debug is set", deletedDevelopers.Len())
	}
	return
}
