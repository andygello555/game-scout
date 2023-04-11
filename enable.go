package main

import (
	"container/heap"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/gotils/v2/numbers"
	"github.com/andygello555/gotils/v2/slices"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"math"
	"sync"
	"time"
)

// EnablePhase will run the "enable" phase of the Scout procedure. This phase enables the top performing developers that
// are currently disabled, but were not disabled in the DisablePhase. The way we find the top-performing developers is by
// finding the trend for each. This is performed on each models.DeveloperType of models.Developer available.
func EnablePhase(state *ScoutState) (err error) {
	scoutResultAny, _ := state.GetCachedField(StateType).Get("Result")
	if err = scoutResultAny.(*models.ScoutResult).EnableStats.Before(db.DB); err != nil {
		log.ERROR.Printf("Could not set Before fields for ScoutResult.EnableStats: %v", err)
		err = nil
	}

	// Fetch the IDs of the developers that were disabled in the DisablePhase.
	disabledDevelopersAny, _ := state.GetCachedField(StateType).Get("DisabledDevelopers")
	disabledDevelopers := disabledDevelopersAny.([]models.DeveloperMinimal)

	// SELECT developers.*
	// FROM (
	//   SELECT developer_snapshots.developer_id, MAX(version) AS latest_version
	//   FROM developer_snapshots
	//   GROUP BY developer_snapshots.developer_id
	// ) AS ds1
	// JOIN developer_snapshots ds2 ON ds1.developer_id = ds2.developer_id AND ds1.latest_version = ds2.version
	// JOIN developers on ds2.developer_id = developers.id
	// WHERE developers.disabled
	//   AND developers.type = <DEVELOPER TYPE>
	//   AND ds1.latest_version > 0
	//   AND developers.id NOT IN <DISABLED DEVELOPERS>
	// ORDER BY ds2.weighted_score DESC;

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
		slices.Comprehension(disabledDevelopers, func(idx int, value models.DeveloperMinimal, arr []models.DeveloperMinimal) string {
			return value.ID
		}),
	).Order(
		"ds2.weighted_score desc",
	).Session(&gorm.Session{})

	var totalCount int64
	if err = query.Select("*").Count(&totalCount).Error; err != nil {
		log.ERROR.Printf(
			"Could not determine the total count of all developers in the main query for the Enabled phase: %v",
			err,
		)
	} else {
		log.INFO.Printf("Total disabled developers to sample from: %d", totalCount)
	}

	// We create a goroutine for each developer type so we can process each concurrently
	var stateMutex sync.Mutex
	start := time.Now().UTC()
	devTypes := models.UnknownDeveloperType.Types()
	errorChannels := make(map[models.DeveloperType]chan error)
	for _, devType := range devTypes {
		errorChannels[devType] = make(chan error)
		go func(devType models.DeveloperType) {
			log.INFO.Printf("Starting Enable worker for %s Developers", devType)
			var err error
			developersToGoAny, _ := state.GetCachedField(StateType).Get(string(devType) + "DevelopersToEnable")
			developersToGo := developersToGoAny.(int)
			if enabledDevelopers, _ := state.GetCachedField(StateType).Get("EnabledDevelopers"); len(enabledDevelopers.([]models.DeveloperMinimal)) == 0 && developersToGo == 0 {
				log.INFO.Printf(
					"%s worker) Looks like State cache does not contain a previous Enable run for %s Developers. Starting from scratch...",
					devType, devType,
				)
				developersToGo = int(math.Ceil(globalConfig.Scrape.Constants.MaxDevelopersToEnable))
				state.GetCachedField(StateType).SetOrAdd("DevelopersToEnable", devType, developersToGo)
				log.INFO.Printf("\t%s worker) maxEnabledDevelopersAfterEnablePhase = %.3f (%.0f)", devType, globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterEnablePhase, globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterEnablePhase)
				log.INFO.Printf("\t%s worker) maxEnabledDevelopersAfterDisablePhase = %.3f (%.0f)", devType, globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterDisablePhase, globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterDisablePhase)
				log.INFO.Printf("\t%s worker) maxDevelopersToEnable = %.3f (%.0f)", devType, globalConfig.Scrape.Constants.MaxDevelopersToEnable, globalConfig.Scrape.Constants.MaxDevelopersToEnable)
			}
			offset := 0
			sample := 0

			for developersToGo > 0 {
				developersToEnable, _ := state.GetCachedField(StateType).Get(string(devType) + "EnabledDevelopers")
				log.INFO.Printf(
					"%s worker) There are %d %s developers to find that should be re-enabled. We have found %d developers to "+
						"re-enable. Limit = %d, Offset = %d, Sample no. = %d",
					devType, developersToGo, devType, len(developersToEnable.([]models.DeveloperMinimal)), developersToGo*2,
					offset, sample+1,
				)

				var developerSample []*models.Developer
				// We fetch twice as many developers as we need because sometimes we cannot run .Trend on the developer
				// successfully. This is also why we keep track of the number of developers we have to go. We finish the loop
				// when we have found developersToGo number of developers, or the query below returns 0 developers.
				// We offset the search each time so that we don't get the same sample each time
				if err = query.Where(
					"developers.type = ?",
					devType,
				).Limit(
					developersToGo * 2,
				).Offset(
					offset,
				).Find(
					&developerSample,
				).Error; err != nil {
					errorChannels[devType] <- errors.Wrapf(
						err, "could not fetch sample of %d %s developers to find out whether we should enable them",
						developersToGo*2, devType,
					)
				}
				log.INFO.Printf(
					"%s worker) We have found %d %s developers to sample for sample no. %d",
					devType, len(developerSample), devType, sample+1,
				)

				if len(developerSample) == 0 {
					log.WARNING.Printf(
						"%s worker) We have run out of %s developers to sample to be re-enabled. There are still %d "+
							"%s developers to go...",
						devType, devType, developersToGo, devType,
					)
					break
				}

				log.INFO.Printf(
					"%s worker) Finding the trends of %d %s developers...",
					devType, len(developerSample), devType,
				)

				// Offset becomes the old limit
				offset += developersToGo * 2

				// Fetch the Trend for each developer and push the results to a trendFinderResultHeap
				developerSortedSample := newTrendFinderResultHeap(false)
				developersToEnableSet := mapset.NewThreadUnsafeSet(developersToEnable.([]models.DeveloperMinimal)...)
				for _, developer := range developerSample {
					if !developersToEnableSet.Contains(models.DeveloperMinimal{ID: developer.ID, Type: developer.Type}) {
						result := &trendFinderResult{
							TrendingDev: &models.TrendingDev{
								Developer: developer,
							},
						}

						if result.Trend, err = developer.Trend(db.DB); err != nil {
							log.WARNING.Printf(
								"%s worker) Could not find trend for %s Developer %v: %v, skipping...",
								devType, devType, developer, err,
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
								"%s worker) We already have found %d/%d trend-ful %s developers in the sample, so "+
									"we are going to stop finding trends for this sample.",
								devType, developerSortedSample.Len(), developersToGo, devType,
							)
							break
						}

						stateMutex.Lock()
						state.GetCachedField(StateType).SetOrAdd("Result", "EnableStats", "TotalSampledDevelopers", models.SetOrAddInc.Func())
						stateMutex.Unlock()
					} else {
						// This should never happen, as the sample is sorted by weighted_score and offset and limit to create
						// distinct batches.
						log.WARNING.Printf(
							"%s worker) EnabledDevelopers already contains ID for %s Developer %v",
							devType, devType, developer,
						)
					}
				}

				// Find the number of developers to pop from the heap. This is the minimum of the length of the heap and the
				// developers remaining.
				developersNeeded := int(math.Min(float64(developerSortedSample.Len()), float64(developersToGo)))
				log.INFO.Printf(
					"%s worker) a = developerSortedSample.Len = %d, b = developersToGo = %d, min(a, b) = %d",
					devType, developerSortedSample.Len(), developersToGo, developersNeeded,
				)
				developersToGo -= developersNeeded

				// Update the DevelopersToEnable count for the developer type
				stateMutex.Lock()
				state.GetCachedField(StateType).SetOrAdd("DevelopersToEnable", devType, developersToGo)
				stateMutex.Unlock()

				for i := 0; i < developersNeeded; i++ {
					developer := heap.Pop(&developerSortedSample).(*trendFinderResult)
					log.INFO.Printf(
						"%s worker) %s Developer %v is placed %s out of %d developers in sample no. %d with coeff[1] = %.10f",
						devType, devType, developer.Developer, numbers.Ordinal(i+1), developersNeeded, sample+1,
						developer.Trend.GetCoeffs()[1],
					)

					stateMutex.Lock()
					state.GetCachedField(StateType).SetOrAdd("EnabledDevelopers", developer.Developer.ID, developer.Developer.Type)
					stateMutex.Unlock()
				}

				// Save the state for security
				if err = state.Save(); err != nil {
					log.ERROR.Printf("Could not save State cache to disk in Enable: %v", err)
				}

				// Increment the sample counters
				stateMutex.Lock()
				state.GetCachedField(StateType).SetOrAdd("Result", "EnableStats", "TotalFinishedSamples", models.SetOrAddInc.Func())
				stateMutex.Unlock()

				sample++
			}

			// Notify the main thread that we have finished with no errors
			errorChannels[devType] <- nil
		}(devType)
	}

	// Wait for the workers for each models.DeveloperType to finish
	for i := 0; i < len(errorChannels); i++ {
		var devType models.DeveloperType
		select {
		case err = <-errorChannels[models.TwitterDeveloperType]:
			devType = models.TwitterDeveloperType
		case err = <-errorChannels[models.RedditDeveloperType]:
			devType = models.RedditDeveloperType
		}

		if err != nil {
			log.ERROR.Printf("Error occurred in Enable worker for %s Developers: %v", devType, err)
			return
		}
		log.INFO.Printf("Enable worker for %s Developers finished in %s", devType, time.Now().UTC().Sub(start).String())
	}

	// Finally, re-enable all the developers of the IDs that were found in the DeveloperType workers above.
	developersToEnable, _ := state.GetCachedField(StateType).Get("EnabledDevelopers")
	developersToEnableArr := slices.Comprehension(developersToEnable.([]models.DeveloperMinimal), func(idx int, value models.DeveloperMinimal, arr []models.DeveloperMinimal) string {
		return value.ID
	})
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

	if err = scoutResultAny.(*models.ScoutResult).EnableStats.After(db.DB); err != nil {
		log.ERROR.Printf("Could not set After fields for ScoutResult.EnableStats: %v", err)
		err = nil
	}
	return
}
