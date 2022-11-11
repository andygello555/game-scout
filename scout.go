package main

import (
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
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
	secondsBetweenDiscoveryBatches = time.Second * 7
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
	discoveryGameScrapeWorkers = 5
	// discoveryMaxConcurrentGameScrapeWorkers is number of scrapeStorefrontsForGameWorker that can be processing a job
	// at the same time in the discovery phase.
	discoveryMaxConcurrentGameScrapeWorkers = 4
	// updateGameScrapeWorkers is the number of scrapeStorefrontsForGameWorker to start in the UpdatePhase.
	updateGameScrapeWorkers = 3
	// updateMaxConcurrentGameScrapeWorkers is number of scrapeStorefrontsForGameWorker that can be processing a job
	// at the same time in the UpdatePhase.
	updateMaxConcurrentGameScrapeWorkers = 2
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
	scoutStart := time.Now().UTC()
	// If the number of discovery tweets we requested is more than maxDiscoveryTweetsDailyPercent of the total tweets
	// available for a day then we will error out.
	if float64(discoveryTweets) > maxTotalDiscoveryTweets {
		return myTwitter.TemporaryErrorf(
			false,
			"cannot request more than %d tweets per day for discovery purposes",
			int(myTwitter.TweetsPerDay),
		)
	}

	// 1st phase: Discovery. Search the recent tweets on Twitter belonging to the hashtags given in config.json.
	userTweetTimes := make(map[string][]time.Time)
	developerSnapshots := make(map[string][]*models.DeveloperSnapshot)
	gameIDs := mapset.NewSet[uuid.UUID]()

	phaseStart := time.Now().UTC()
	log.INFO.Println("Starting Discovery phase")

	var subGameIDs mapset.Set[uuid.UUID]
	if subGameIDs, err = DiscoveryPhase(batchSize, discoveryTweets, userTweetTimes, developerSnapshots); err != nil {
		return err
	}
	gameIDs = gameIDs.Union(subGameIDs)

	log.INFO.Printf("Finished Discovery phase in %s", time.Now().UTC().Sub(phaseStart).String())

	// 2nd phase: Update. For any developers, and games that exist in the DB but haven't been scraped we will fetch the
	// details from the DB and initiate scrapes for them
	log.INFO.Printf("We have scraped:")
	log.INFO.Printf("\t%d developers", len(developerSnapshots))
	log.INFO.Printf("\t%d games", gameIDs.Cardinality())

	phaseStart = time.Now().UTC()
	log.INFO.Printf("Starting Update phase")
	// Extract the IDs of the developers that were just scraped in the discovery phase.
	i := 0
	scrapedDevelopers := make([]string, len(developerSnapshots))
	for id := range developerSnapshots {
		scrapedDevelopers[i] = id
		i++
	}

	// Run the UpdatePhase.
	if subGameIDs, err = UpdatePhase(batchSize, discoveryTweets, scrapedDevelopers, userTweetTimes, developerSnapshots); err != nil {
		return err
	}
	gameIDs = gameIDs.Union(subGameIDs)
	log.INFO.Printf("Finished Update phase in %s", time.Now().UTC().Sub(phaseStart).String())

	// 3rd phase: Snapshot. For all the partial DeveloperSnapshots that exist in the developerSnapshots map we will
	// aggregate the information for them.
	phaseStart = time.Now().UTC()
	log.INFO.Printf("Starting Snapshot phase. Aggregating %d DeveloperSnapshots", len(developerSnapshots))
	for id, snapshots := range developerSnapshots {
		aggregatedSnap := &models.DeveloperSnapshot{
			DeveloperID:                  id,
			Tweets:                       int32(len(userTweetTimes[id])),
			TweetTimeRange:               models.NullDurationFromPtr(nil),
			LastTweetTime:                userTweetTimes[id][0].UTC(),
			AverageDurationBetweenTweets: models.NullDurationFromPtr(nil),
			TweetsPublicMetrics:          &twitter.TweetMetricsObj{},
			UserPublicMetrics:            &twitter.UserMetricsObj{},
			ContextAnnotationSet:         myTwitter.NewContextAnnotationSet(),
		}

		// If we have more than one tweet, then we will calculate TweetTimeRange, and AverageDurationBetweenTweets
		if aggregatedSnap.Tweets > 1 {
			// Sort the tweet times for this user.
			sort.Slice(userTweetTimes[id], func(i, j int) bool {
				return userTweetTimes[id][i].Before(userTweetTimes[id][j])
			})
			// Find the max and min times by looking at the head and end of array
			maxTime := userTweetTimes[id][len(userTweetTimes[id])-1]
			minTime := userTweetTimes[id][0]
			aggregatedSnap.LastTweetTime = maxTime.UTC()
			// Aggregate the durations between all the tweets
			betweenCount := time.Nanosecond * 0
			for t, tweetTime := range userTweetTimes[id] {
				// Aggregate the duration between this time and the last time
				if t > 0 {
					betweenCount += tweetTime.Sub(userTweetTimes[id][t-1])
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
	}
	log.INFO.Printf("Finished aggregating DeveloperSnapshots in %s", time.Now().UTC().Sub(phaseStart).String())

	// 4th phase: Disable. Disable the developers who aren't doing so well. The number of developers to disable depends
	// on the number of tweets we get to use in the update phase the next time around.
	phaseStart = time.Now().UTC()
	log.INFO.Println("Starting Disable phase")

	if err = DisablePhase(); err != nil {
		return errors.Wrap(err, "disable phase has failed")
	}

	log.INFO.Printf("Finished Disable phase in %s", time.Now().UTC().Sub(phaseStart))

	// 5th phase: Measure. For all the users that have a good number of snapshots we'll see if any are shining above the
	// rest

	log.INFO.Printf("Finished Scout in %s", time.Now().UTC().Sub(scoutStart))
	return
}
