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
	// secondsBetweenBatches is the number of seconds to sleep between DiscoveryBatch batches.
	secondsBetweenBatches = time.Second * 20
	// maxTotalDiscoveryTweetsDailyPercent is the maximum percentage that the discoveryTweets number can be out of
	// myTwitter.TweetsPerDay.
	maxTotalDiscoveryTweetsDailyPercent = 0.55
	// maxTotalDiscoveryTweets is the maximum number of discoveryTweets that can be given to Scout.
	maxTotalDiscoveryTweets = float64(myTwitter.TweetsPerDay) * maxTotalDiscoveryTweetsDailyPercent
	// maxTotalUpdateTweets is the maximum number of tweets that can be scraped by the Update phase.
	maxTotalUpdateTweets = float64(myTwitter.TweetsPerDay) * (1.0 - maxTotalDiscoveryTweetsDailyPercent)
	// maxNonDisabledDevelopers is the number of developers to keep in the Disable phase.
	maxNonDisabledDevelopers = maxTotalUpdateTweets / maxUpdateTweets
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
func Scout(batchSize int, discoveryTweets int) (err error) {
	scoutStart := time.Now().UTC()
	// If the number of discovery tweets we requested is more than maxDiscoveryTweetsDailyPercent of the total tweets
	// available for a day then we will error out.
	if float64(discoveryTweets) > maxTotalDiscoveryTweets {
		return myTwitter.TemporaryErrorf(false, "cannot request more than %d tweets per day for discovery purposes", int(myTwitter.TweetsPerDay))
	}

	// 1st phase: Discovery. Search the recent tweets on Twitter belonging to the hashtags given in config.json.
	query := globalConfig.Twitter.TwitterQuery()
	opts := twitter.TweetRecentSearchOpts{
		Expansions: []twitter.Expansion{
			twitter.ExpansionEntitiesMentionsUserName,
			twitter.ExpansionAuthorID,
			twitter.ExpansionReferencedTweetsID,
			twitter.ExpansionReferencedTweetsIDAuthorID,
			twitter.ExpansionInReplyToUserID,
		},
		TweetFields: []twitter.TweetField{
			twitter.TweetFieldCreatedAt,
			twitter.TweetFieldConversationID,
			twitter.TweetFieldAttachments,
			twitter.TweetFieldPublicMetrics,
			twitter.TweetFieldReferencedTweets,
			twitter.TweetFieldContextAnnotations,
			twitter.TweetFieldEntities,
			twitter.TweetFieldAuthorID,
		},
		UserFields: []twitter.UserField{
			twitter.UserFieldDescription,
			twitter.UserFieldEntities,
			twitter.UserFieldPublicMetrics,
			twitter.UserFieldVerified,
			twitter.UserFieldPinnedTweetID,
			twitter.UserFieldCreatedAt,
		},
		SortOrder: twitter.TweetSearchSortOrderRecency,
	}

	userTweetTimes := make(map[string][]time.Time)
	developerSnapshots := make(map[string][]*models.DeveloperSnapshot)
	gameIDs := mapset.NewSet[uuid.UUID]()

	batchNo := 1
	phaseStart := time.Now().UTC()
	log.INFO.Println("Starting Discovery phase")
	for i := 0; i < discoveryTweets; i += batchSize {
		batchStart := time.Now().UTC()
		offset := batchSize
		if i+offset > discoveryTweets {
			offset = discoveryTweets - i
		}
		opts.MaxResults = offset

		log.INFO.Printf("Executing RecentSearch binding for batch no. %d (%d/%d) for %d tweets", batchNo, i, discoveryTweets, offset)
		var result myTwitter.BindingResult
		if result, err = myTwitter.Client.ExecuteBinding(myTwitter.RecentSearch, &myTwitter.BindingOptions{Total: offset}, query, opts); err != nil {
			log.ERROR.Printf("Could not fetch %d tweets in Scout on batch %d/%d: %s. Continuing...", offset, i, discoveryTweets, err.Error())
			continue
		}

		// The sub-phase of the Discovery phase is the cleansing phase
		tweetRaw := result.Raw().(*twitter.TweetRaw)

		log.INFO.Printf("Executing DiscoveryBatch for batch no. %d (%d/%d) for %d tweets", batchNo, i, discoveryTweets, len(tweetRaw.TweetDictionaries()))
		// Run DiscoveryBatch for this batch of tweet dictionaries
		// If the error is not temporary then we will kill the Scouting process
		var subGameIDs mapset.Set[uuid.UUID]
		if subGameIDs, err = DiscoveryBatch(batchNo, tweetRaw.TweetDictionaries(), userTweetTimes, developerSnapshots); err != nil && !myTwitter.IsTemporary(err) {
			log.FATAL.Printf("Error returned by DiscoveryBatch is not temporary: %s. We have to stop :(", err.Error())
			return errors.Wrapf(err, "could not execute DiscoveryBatch for batch no. %d", batchNo)
		}
		gameIDs = gameIDs.Union(subGameIDs)

		// Set up the next batch of requests by finding the NextToken of the current batch
		opts.NextToken = result.Meta().NextToken()
		batchNo++
		log.INFO.Printf("Set NextToken for next batch (%d) to %s by getting the NextToken from the previous result", batchNo, opts.NextToken)

		// Nap time
		log.INFO.Printf(
			"Finished batch in %s. Sleeping for %s so we don't overdo it",
			time.Now().UTC().Sub(batchStart).String(), secondsBetweenBatches.String(),
		)
		sleepBar(secondsBetweenBatches)
	}
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
	if err = UpdatePhase(batchSize, discoveryTweets, scrapedDevelopers, userTweetTimes, developerSnapshots, gameIDs); err != nil {
		return err
	}
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

	phaseStart = time.Now().UTC()
	log.INFO.Println("Starting Disable phase")

	var enabledDeveloperCount int64
	if err = db.DB.Model(&models.Developer{}).Where("NOT disabled").Count(&enabledDeveloperCount).Error; err == nil {
		log.INFO.Printf("There are %d enabled developers", enabledDeveloperCount)
		if maxEnabled := int64(math.Floor(maxNonDisabledDevelopers)); enabledDeveloperCount > maxEnabled {
			limit := enabledDeveloperCount - maxEnabled
			log.INFO.Printf("Disabling %d of the worst performing developers", limit)
			if update := db.DB.Model(&models.Developer{}).Update(
				"disabled", true,
			).Where(
				"id IN (?)",
				db.DB.Table(
					"(?) as ds1",
					db.DB.Model(&models.DeveloperSnapshot{}).Select(
						"developer_snapshots.developer_id", "max(version) as latest_version",
					).Group("developer_snapshots.developer_id"),
				).Joins(
					"JOIN developer_snapshots ds2 ON ds1.developer_id = ds2.developer_id AND ds1.latest_version = ds2.version",
				).Joins(
					"JOIN developers on ds2.developer_id = developers.id",
				).Where("NOT developers.disabled").Order("ds2.weighted_score").Limit(int(limit)),
			); update.Error != nil {
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

	log.INFO.Printf("Finished Disable phase in %s", time.Now().UTC().Sub(phaseStart))

	// 5th phase: Measure. For all the users that have a good number of snapshots we'll see if any are shining above the
	// rest

	log.INFO.Printf("Finished Scout in %s", time.Now().UTC().Sub(scoutStart))
	return
}
