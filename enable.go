package main

import (
	"container/heap"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/gotils/v2/numbers"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	"math"
)

// EnablePhase will run the "enable" phase of the Scout procedure. This phase enables the top performing developers that
// are currently disabled, but were not disabled in the DisablePhase. The way we find the top-performing developers is by
// finding the trend for each.
func EnablePhase(state *ScoutState) (err error) {
	// Fetch the IDs of the developers that were disabled in the DisablePhase.
	disabledDevelopersAny, _ := state.GetCachedField(StateType).Get("DisabledDevelopers")
	disabledDevelopers := disabledDevelopersAny.([]string)

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
		"developers.disabled AND ds1.latest_version > 0 AND developers.id NOT IN ?",
		disabledDevelopers,
	).Order(
		"ds2.weighted_score desc",
	)

	var totalCount int64
	if err = query.Select("*").Count(&totalCount).Error; err != nil {
		log.ERROR.Printf(
			"Could not determine the total count of all developers in the main query for the Enabled phase: %v",
			err,
		)
	} else {
		log.INFO.Printf("Total disabled developers to sample from: %d", totalCount)
	}

	developersToGoAny, _ := state.GetCachedField(StateType).Get("DevelopersToEnable")
	developersToGo := developersToGoAny.(int)
	if enabledDevelopers, _ := state.GetCachedField(StateType).Get("EnabledDevelopers"); len(enabledDevelopers.([]string)) == 0 && developersToGo == 0 {
		log.INFO.Printf("Looks like State cache does not contain a previous Enable run. Starting from scratch...")
		developersToGo = int(math.Ceil(maxDevelopersToEnable))
		state.GetCachedField(StateType).SetOrAdd("DevelopersToEnable", developersToGo)
		log.INFO.Printf("\tmaxEnabledDevelopersAfterEnablePhase = %.3f (%.0f)", maxEnabledDevelopersAfterEnablePhase, maxEnabledDevelopersAfterEnablePhase)
		log.INFO.Printf("\tmaxEnabledDevelopersAfterDisablePhase = %.3f (%.0f)", maxEnabledDevelopersAfterDisablePhase, maxEnabledDevelopersAfterDisablePhase)
		log.INFO.Printf("\tmaxDevelopersToEnable = %.3f (%.0f)", maxDevelopersToEnable, maxDevelopersToEnable)
	}
	offset := 0
	sample := 0

	for developersToGo > 0 {
		developersToEnable, _ := state.GetCachedField(StateType).Get("EnabledDevelopers")
		log.INFO.Printf(
			"There are %d developers to find that should be re-enabled. We have found %d developers to re-enable. "+
				"Limit = %d, Offset = %d, Sample no. = %d",
			developersToGo, len(developersToEnable.([]string)), developersToGo*2, offset, sample+1,
		)

		var developerSample []*models.Developer
		// We fetch twice as many developers as we need because sometimes we cannot run .Trend on the developer
		// successfully. This is also why we keep track of the number of developers we have to go. We finish the loop
		// when we have found developersToGo number of developers, or the query below returns 0 developers.
		// We offset the search each time so that we don't get the same sample each time
		if err = query.Limit(developersToGo * 2).Offset(offset).Find(&developerSample).Error; err != nil {
			return errors.Wrapf(
				err,
				"could not fetch sample of %d developers to find out whether we should enable them",
				developersToGo*2,
			)
		}
		log.INFO.Printf("We have found %d developers to sample for sample no. %d", len(developerSample), sample+1)

		if len(developerSample) == 0 {
			log.WARNING.Printf(
				"We have run out of developers to sample to be re-enabled. There are still %d developers to go...",
				developersToGo,
			)
			break
		}

		log.INFO.Printf("Finding the trends of %d developers...", len(developerSample))

		// Offset becomes the old limit
		offset += developersToGo * 2

		// Fetch the Trend for each developer and push the results to a trendFinderResultHeap
		developerSortedSample := newTrendFinderResultHeap(false)
		for _, developer := range developerSample {
			if !mapset.NewThreadUnsafeSet(developersToEnable.([]string)...).Contains(developer.ID) {
				result := &trendFinderResult{
					TrendingDev: &models.TrendingDev{
						Developer: developer,
					},
				}

				if result.Trend, err = developer.Trend(db.DB); err != nil {
					log.WARNING.Printf(
						"Could not find trend for Developer %v: %v, skipping...",
						developer, err,
					)
					continue
				}

				heap.Push(&developerSortedSample, result)

				// We know that any developer added to the heap has successfully been fetched, so we break out of this loop
				// if we have already reached the total remaining developers that we need. The only time this check should
				// never succeed is when the heap contains less than developersToGo developers that can successfully call
				// .Trend.
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
				log.WARNING.Printf("EnabledDevelopers already contains ID for Developer %v", developer)
			}
		}

		// Find the number of developers to pop from the heap. This is the minimum of the length of the heap and the
		// developers remaining.
		developersNeeded := int(math.Min(float64(developerSortedSample.Len()), float64(developersToGo)))
		log.INFO.Printf(
			"a = developerSortedSample.Len = %d, b = developersToGo = %d, min(a, b) = %d",
			developerSortedSample.Len(), developersToGo, developersNeeded,
		)
		developersToGo -= developersNeeded
		state.GetCachedField(StateType).SetOrAdd("DevelopersToEnable", developersToGo)
		for i := 0; i < developersNeeded; i++ {
			developer := heap.Pop(&developerSortedSample).(*trendFinderResult)
			log.INFO.Printf(
				"Developer %v is placed %s out of %d developers in sample no. %d with coeff[1] = %.10f",
				developer.Developer, numbers.Ordinal(i+1), developersNeeded, sample+1,
				developer.Trend.GetCoeffs()[1],
			)
			state.GetCachedField(StateType).SetOrAdd("EnabledDevelopers", developer.Developer.ID)
		}
		if err = state.Save(); err != nil {
			log.ERROR.Printf("Could not save State cache to disk in Enable: %v", err)
		}
		sample++
	}

	// Finally, re-enable all the developers of the IDs that were found above.
	developersToEnable, _ := state.GetCachedField(StateType).Get("EnabledDevelopers")
	developersToEnableArr := developersToEnable.([]string)
	log.INFO.Printf("Re-enabling %d developers", len(developersToEnableArr))
	if len(developersToEnableArr) > 0 {
		if err = db.DB.Model(
			&models.Developer{},
		).Where(
			"id IN ?", developersToEnableArr,
		).Update(
			"disabled", false,
		).Error; err != nil {
			err = errors.Wrapf(err, "could not re-enable %d developers", len(developersToEnableArr))
			return
		}
	}
	return
}