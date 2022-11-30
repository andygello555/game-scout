package main

import (
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myErrors "github.com/andygello555/game-scout/errors"
	myTwitter "github.com/andygello555/game-scout/twitter"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
	"math"
	"sort"
	"time"
)

const (
	// transformTweetWorkers is the number of transformTweetWorker that will be spun up in the DiscoveryBatch.
	transformTweetWorkers = 5
	// updateDeveloperWorkers is the number of updateDeveloperWorker that will be spun up in the update phase.
	updateDeveloperWorkers = 5
	// maxUpdateTweets is the maximum number of tweets fetched in the update phase.
	maxUpdateTweets = 12
	// secondsBetweenDiscoveryBatches is the number of seconds to sleep between DiscoveryBatch batches.
	secondsBetweenDiscoveryBatches = time.Second * 3
	// secondsBetweenUpdateBatches is the number of seconds to sleep between queue batches of updateDeveloperJob.
	secondsBetweenUpdateBatches = time.Second * 30
	// maxTotalDiscoveryTweetsDailyPercent is the maximum percentage that the discoveryTweets number can be out of
	// myTwitter.TweetsPerDay.
	maxTotalDiscoveryTweetsDailyPercent = 0.55
	// maxTotalDiscoveryTweets is the maximum number of discoveryTweets that can be given to Scout.
	maxTotalDiscoveryTweets = float64(myTwitter.TweetsPerDay) * maxTotalDiscoveryTweetsDailyPercent
	// maxTotalUpdateTweets is the maximum number of tweets that can be scraped by the Update phase.
	maxTotalUpdateTweets = float64(myTwitter.TweetsPerDay) * (1.0 - maxTotalDiscoveryTweetsDailyPercent)
	// maxEnabledDevelopers is the number of developers to keep in the Disable phase.
	maxEnabledDevelopers = maxTotalUpdateTweets / maxUpdateTweets
	// discoveryGameScrapeWorkers is the number of scrapeStorefrontsForGameWorker to start in the discovery phase.
	discoveryGameScrapeWorkers = 10
	// discoveryMaxConcurrentGameScrapeWorkers is number of scrapeStorefrontsForGameWorker that can be processing a job
	// at the same time in the discovery phase.
	discoveryMaxConcurrentGameScrapeWorkers = 9
	// updateGameScrapeWorkers is the number of scrapeStorefrontsForGameWorker to start in the UpdatePhase.
	updateGameScrapeWorkers = 10
	// updateMaxConcurrentGameScrapeWorkers is number of scrapeStorefrontsForGameWorker that can be processing a job
	// at the same time in the UpdatePhase.
	updateMaxConcurrentGameScrapeWorkers = 9
)

// sleepBar will sleep for the given time.Duration (as seconds) and display a progress bar for the sleep.
func sleepBar(sleepDuration time.Duration) {
	bar := progressbar.Default(int64(math.Ceil(sleepDuration.Seconds())))
	for s := 0; s < int(math.Ceil(sleepDuration.Seconds())); s++ {
		_ = bar.Add(1)
		time.Sleep(time.Second)
	}
}

// Scout will run the 5 phase scraping procedure for the game scout system.
//
// • 1st phase: Discovery. Search the recent tweets on Twitter belonging to the hashtags given in config.json.
//
// • 2nd phase: Update. For any developers, and games that exist in the DB that haven't been scraped in the Discovery
// phase, we will fetch their details from the DB and initiate scrapes for them to update their models.Game, create a
// models.DeveloperSnapshot, and update the models.Developer instance itself.
//
// • 3rd phase: Snapshot. For all the partial models.DeveloperSnapshot that have been created in the previous two phases
// we will aggregate the information for them into one models.DeveloperSnapshot for each models.Developer.
//
// • 4th phase: Disable. Disable all the models.Developer who aren't doing so well. The number of developers to disable
// depends on the number of tweets we get to use in the update phase the next time around:
// totalEnabledDevelopers - maxEnabledDevelopers.
//
// • 5th phase: Measure. For all the models.Developer that have a good number of models.DeveloperSnapshot we'll see if
// any are shining above the rest.
func Scout(batchSize int, discoveryTweets int) (err error) {
	var state *ScoutState
	if state, err = StateLoadOrCreate(false); err != nil {
		return errors.Wrapf(err, "could not load/create ScoutState when starting Scout procedure")
	}

	defer func() {
		// If an error occurs we'll always attempt to save the state
		if err != nil {
			err = myErrors.MergeErrors(err, state.Save())
		}
	}()

	if state.Loaded {
		log.WARNING.Printf("Previous ScoutState \"%s\" was loaded from disk:", state.BaseDir())
		log.WARNING.Println(state.String())
		batchSizeAny, _ := state.GetCachedField(StateType).Get("BatchSize")
		discoveryTweetsAny, _ := state.GetCachedField(StateType).Get("DiscoveryTweets")
		batchSize = batchSizeAny.(int)
		discoveryTweets = discoveryTweetsAny.(int)
	} else {
		// If no state has been loaded, then we'll assume that the previous run of Scout was successful and set up
		// ScoutState as usual
		state.GetCachedField(StateType).SetOrAdd("Phase", Discovery)
		state.GetCachedField(StateType).SetOrAdd("BatchSize", batchSize)
		state.GetCachedField(StateType).SetOrAdd("DiscoveryTweets", discoveryTweets)
	}

	var phaseStart time.Time
	phaseBefore := func() {
		phaseStart = time.Now().UTC()
		state.GetCachedField(StateType).SetOrAdd("PhaseStart", phaseStart)
		phase, _ := state.GetCachedField(StateType).Get("Phase")
		log.INFO.Printf("Starting %s phase", phase.(Phase).String())
	}

	phaseAfter := func() {
		phase, _ := state.GetCachedField(StateType).Get("Phase")
		log.INFO.Printf("Finished %s phase in %s", phase.(Phase).String(), time.Now().UTC().Sub(phaseStart).String())
		nextPhase := phase.(Phase).Next()
		state.GetCachedField(StateType).SetOrAdd("Phase", nextPhase)
		if err = state.Save(); err != nil {
			log.ERROR.Printf("Could not save ScoutState: %v", err)
		}
	}

	state.GetCachedField(StateType).SetOrAdd("Start", time.Now().UTC())
	if phase, _ := state.GetCachedField(StateType).Get("Phase"); phase.(Phase) == Discovery {
		// If the number of discovery tweets we requested is more than maxDiscoveryTweetsDailyPercent of the total tweets
		// available for a day then we will error out.
		if float64(discoveryTweets) > maxTotalDiscoveryTweets {
			return myErrors.TemporaryErrorf(
				false,
				"cannot request more than %d tweets per day for discovery purposes",
				int(myTwitter.TweetsPerDay),
			)
		}

		phaseBefore()

		// 1st phase: Discovery. Search the recent tweets on Twitter belonging to the hashtags given in config.json.
		var subGameIDs mapset.Set[uuid.UUID]
		if subGameIDs, err = DiscoveryPhase(state); err != nil {
			return err
		}
		state.GetIterableCachedField(GameIDsType).Merge(&GameIDs{subGameIDs})

		phaseAfter()
	}

	if phase, _ := state.GetCachedField(StateType).Get("Phase"); phase.(Phase) == Update {
		// 2nd phase: Update. For any developers, and games that exist in the DB but haven't been scraped we will fetch the
		// details from the DB and initiate scrapes for them
		log.INFO.Printf("We have scraped:")
		log.INFO.Printf("\t%d developers", state.GetIterableCachedField(DeveloperSnapshotsType).Len())
		log.INFO.Printf("\t%d games", state.GetIterableCachedField(DeveloperSnapshotsType).Len())

		phaseBefore()

		// Extract the IDs of the developers that were just scraped in the discovery phase.
		scrapedDevelopers := make([]string, state.GetIterableCachedField(DeveloperSnapshotsType).Len())
		iter := state.GetIterableCachedField(DeveloperSnapshotsType).Iter()
		for iter.Continue() {
			scrapedDevelopers[iter.I()] = iter.Key().(string)
			iter.Next()
		}

		// If there are already developers that have been updated in a previously run UpdatePhase, then we will add
		// these to the scrapedDevelopers also
		stateState := state.GetCachedField(StateType).(*State)
		if len(stateState.UpdatedDevelopers) > 0 {
			scrapedDevelopers = append(scrapedDevelopers, stateState.UpdatedDevelopers...)
		}

		// Run the UpdatePhase.
		if err = UpdatePhase(scrapedDevelopers, state); err != nil {
			return err
		}

		phaseAfter()
	}

	if phase, _ := state.GetCachedField(StateType).Get("Phase"); phase.(Phase) == Snapshot {
		// 3rd phase: Snapshot. For all the partial DeveloperSnapshots that exist in the developerSnapshots map we will
		// aggregate the information for them.
		phaseBefore()

		dsIter := state.GetIterableCachedField(DeveloperSnapshotsType).Iter()
		for dsIter.Continue() {
			idAny := dsIter.Key()
			snapshotsAny, _ := state.GetIterableCachedField(DeveloperSnapshotsType).Get(idAny)
			userTweetTimesAny, _ := state.GetIterableCachedField(UserTweetTimesType).Get(idAny)

			id := idAny.(string)
			snapshots := snapshotsAny.([]*models.DeveloperSnapshot)
			userTweetTimes := userTweetTimesAny.([]time.Time)

			aggregatedSnap := &models.DeveloperSnapshot{
				DeveloperID:                  id,
				Tweets:                       int32(len(userTweetTimes)),
				TweetTimeRange:               models.NullDurationFromPtr(nil),
				LastTweetTime:                userTweetTimes[0].UTC(),
				AverageDurationBetweenTweets: models.NullDurationFromPtr(nil),
				TweetsPublicMetrics:          &twitter.TweetMetricsObj{},
				UserPublicMetrics:            &twitter.UserMetricsObj{},
				ContextAnnotationSet:         myTwitter.NewContextAnnotationSet(),
			}

			// If we have more than one tweet, then we will calculate TweetTimeRange, and AverageDurationBetweenTweets
			if aggregatedSnap.Tweets > 1 {
				// Sort the tweet times for this user.
				sort.Slice(userTweetTimes, func(i, j int) bool {
					return userTweetTimes[i].Before(userTweetTimes[j])
				})
				// Find the max and min times by looking at the head and end of array
				maxTime := userTweetTimes[len(userTweetTimes)-1]
				minTime := userTweetTimes[0]
				aggregatedSnap.LastTweetTime = maxTime.UTC()
				// Aggregate the durations between all the tweets
				betweenCount := time.Nanosecond * 0
				for t, tweetTime := range userTweetTimes {
					// Aggregate the duration between this time and the last time
					if t > 0 {
						betweenCount += tweetTime.Sub(userTweetTimes[t-1])
					}
				}
				timeRange := maxTime.Sub(minTime)
				aggregatedSnap.TweetTimeRange = models.NullDurationFromPtr(&timeRange)
				averageDurationBetween := time.Duration(int64(betweenCount) / int64(aggregatedSnap.Tweets))
				aggregatedSnap.AverageDurationBetweenTweets = models.NullDurationFromPtr(&averageDurationBetween)
			}

			// We then aggregate the tweet/user public metrics for each tweet as well as context annotations.
			for _, snapshot := range snapshots {
				aggregatedSnap.TweetsPublicMetrics.Impressions += snapshot.TweetsPublicMetrics.Impressions
				aggregatedSnap.TweetsPublicMetrics.URLLinkClicks += snapshot.TweetsPublicMetrics.URLLinkClicks
				aggregatedSnap.TweetsPublicMetrics.UserProfileClicks += snapshot.TweetsPublicMetrics.UserProfileClicks
				aggregatedSnap.TweetsPublicMetrics.Likes += snapshot.TweetsPublicMetrics.Likes
				aggregatedSnap.TweetsPublicMetrics.Replies += snapshot.TweetsPublicMetrics.Replies
				aggregatedSnap.TweetsPublicMetrics.Retweets += snapshot.TweetsPublicMetrics.Retweets
				aggregatedSnap.TweetsPublicMetrics.Quotes += snapshot.TweetsPublicMetrics.Quotes
				aggregatedSnap.UserPublicMetrics.Followers += snapshot.UserPublicMetrics.Followers
				aggregatedSnap.UserPublicMetrics.Following += snapshot.UserPublicMetrics.Following
				aggregatedSnap.UserPublicMetrics.Tweets += snapshot.UserPublicMetrics.Tweets
				aggregatedSnap.UserPublicMetrics.Listed += snapshot.UserPublicMetrics.Listed
				it := snapshot.ContextAnnotationSet.Iterator()
				for elem := range it.C {
					aggregatedSnap.ContextAnnotationSet.Add(elem)
				}
			}
			if err = db.DB.Create(aggregatedSnap).Error; err != nil {
				return errors.Wrapf(err, "could not save aggregation of %d DeveloperSnapshots for dev. %s", len(snapshots), id)
			}
			log.INFO.Printf("\tSaved DeveloperSnapshot aggregation for %s which is comprised of %d partial snapshots", id, len(snapshots))
			dsIter.Next()
		}

		phaseAfter()
	}

	if phase, _ := state.GetCachedField(StateType).Get("Phase"); phase.(Phase) == Disable {
		// 4th phase: Disable. Disable the developers who aren't doing so well. The number of developers to disable depends
		// on the number of tweets we get to use in the update phase the next time around.
		phaseBefore()

		if err = DisablePhase(); err != nil {
			return errors.Wrap(err, "disable phase has failed")
		}

		phaseAfter()
	}

	if phase, _ := state.GetCachedField(StateType).Get("Phase"); phase.(Phase) == Measure {
		// 5th phase: Measure. For all the users that have a good number of snapshots we'll see if any are shining above the
		// rest
		phaseBefore()
		phaseAfter()
	}

	state.GetCachedField(StateType).SetOrAdd("Continue", time.Now().UTC())
	start, _ := state.GetCachedField(StateType).Get("Start")
	finished, _ := state.GetCachedField(StateType).Get("Continue")
	log.INFO.Printf("Finished Scout in %s", finished.(time.Time).Sub(start.(time.Time)).String())

	state.Delete()
	return
}
