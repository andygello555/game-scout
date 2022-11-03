package main

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myTwitter "github.com/andygello555/game-scout/twitter"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"reflect"
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

// TransformTweet takes a twitter.TweetDictionary and splits it out into instances of the models.Developer,
// models.DeveloperSnapshot, and models.Game models. It also returns the time when the tweet was created so that we can
// track the duration between tweets for models.DeveloperSnapshot.
func TransformTweet(tweet *twitter.TweetDictionary) (developer *models.Developer, developerSnap *models.DeveloperSnapshot, game *models.Game, tweetCreatedAt time.Time, err error) {
	developer = &models.Developer{}
	developerSnap = &models.DeveloperSnapshot{}
	// If there are referenced tweets we will check if any are the retweeted tweet. If so then we will look at this
	// tweet instead
	if len(tweet.ReferencedTweets) > 0 {
		for _, referencedTweet := range tweet.ReferencedTweets {
			if referencedTweet.Reference.Type == "retweeted" {
				tweet = referencedTweet.TweetDictionary
				break
			}
		}
	}

	if tweetCreatedAt, err = time.Parse(myTwitter.CreatedAtFormat, tweet.Tweet.CreatedAt); err != nil {
		err = errors.Wrap(err, "could not parse tweet's created_at")
		return
	}

	developer.ID = tweet.Author.ID
	developerSnap.DeveloperID = developer.ID
	developer.Name = tweet.Author.Name
	developer.Username = tweet.Author.UserName
	developer.PublicMetrics = tweet.Author.PublicMetrics
	developer.Description = tweet.Author.Description
	if developer.ProfileCreated, err = time.Parse(myTwitter.CreatedAtFormat, tweet.Author.CreatedAt); err != nil {
		err = errors.Wrap(err, "could not parse author's created_at")
		return
	}

	// Next we find if there are any SteamURLAppPages that are linked in the tweet
	storefrontMap := make(map[models.Storefront]mapset.Set[string])
	for _, url := range tweet.Tweet.Entities.URLs {
		for _, check := range []struct {
			url        browser.ScrapeURL
			storefront models.Storefront
		}{
			{browser.SteamAppPage, models.SteamStorefront},
		} {
			if check.url.Match(url.ExpandedURL) {
				if _, ok := storefrontMap[check.storefront]; !ok {
					storefrontMap[check.storefront] = mapset.NewSet[string]()
				}
				storefrontMap[check.storefront].Add(url.ExpandedURL)
			}
		}
	}

	// Scrape metrics from all the storefronts found for the game.
	game = models.ScrapeStorefrontsForGame(storefrontMap)
	if game != nil {
		game.DeveloperID = developer.ID
	}

	developerSnap.TweetsPublicMetrics = tweet.Tweet.PublicMetrics
	developerSnap.UserPublicMetrics = developer.PublicMetrics
	developerSnap.ContextAnnotationSet = myTwitter.NewContextAnnotationSet(tweet.Tweet.ContextAnnotations...)
	return
}

// transformTweetJob wraps the arguments of TransformTweet for transformTweetWorker.
type transformTweetJob struct {
	tweetNo int
	tweet   *twitter.TweetDictionary
}

// transformTweetResult wraps the return values of TransformTweet for transformTweetWorker.
type transformTweetResult struct {
	tweetNo        int
	developer      *models.Developer
	developerSnap  *models.DeveloperSnapshot
	game           *models.Game
	tweetCreatedAt time.Time
	err            error
}

// transformTweetWorker takes a channel of twitter.TweetDictionary, and queues up the return values of TransformTweet as
// transformTweetResult in a results channel.
func transformTweetWorker(jobs <-chan *transformTweetJob, results chan<- *transformTweetResult) {
	for job := range jobs {
		result := &transformTweetResult{}
		result.developer, result.developerSnap, result.game, result.tweetCreatedAt, result.err = TransformTweet(job.tweet)
		result.tweetNo = job.tweetNo
		results <- result
	}
}

func DiscoveryBatch(batchNo int, dictionary map[string]*twitter.TweetDictionary, userTweetTimes map[string][]time.Time, developerSnapshots map[string][]*models.DeveloperSnapshot) (gameIDs mapset.Set[uuid.UUID], err error) {
	logPrefix := fmt.Sprintf("DiscoveryBatch %d: ", batchNo)
	updateOrCreateModel := func(value any) (created bool) {
		created = false
		// First we do a null check
		var updateOrCreate *gorm.DB
		switch value.(type) {
		case *models.Game:
			if value.(*models.Game) == nil {
				return
			}
		case *models.Developer:
			if value.(*models.Developer) == nil {
				return
			}
		}

		type upsertable interface {
			OnConflict() clause.OnConflict
		}
		// Then we construct the upsert clause
		onConflict := value.(upsertable).OnConflict()
		onConflict.DoUpdates = clause.AssignmentColumns(db.GetModel(value).ColumnDBNamesExcluding("id"))
		if updateOrCreate = db.DB.Clauses(onConflict); updateOrCreate.Error != nil {
			err = myTwitter.TemporaryWrapf(false, updateOrCreate.Error, "could not create update or create clause for %s", reflect.TypeOf(value).Elem().Name())
			return
		}

		// Finally, we create the model instance
		if after := updateOrCreate.Create(value); after.Error != nil {
			err = after.Error
		}
		log.INFO.Printf("%screated %s", logPrefix, reflect.TypeOf(value).Elem().Name())
		created = true
		return
	}

	log.INFO.Printf("Starting DiscoveryBatch no. %d", batchNo)

	// Create the job and result channels for the transformTweetWorkers
	jobs := make(chan *transformTweetJob, len(dictionary))
	results := make(chan *transformTweetResult, len(dictionary))
	gameIDs = mapset.NewSet[uuid.UUID]()

	// Start the workers
	for i := 0; i < transformTweetWorkers; i++ {
		go transformTweetWorker(jobs, results)
	}

	// Queue up all the jobs
	tweetNo := 1
	for id, tweet := range dictionary {
		log.INFO.Printf("%squeued tweet no. %d (%s)", logPrefix, tweetNo, id)
		jobs <- &transformTweetJob{
			tweetNo: tweetNo,
			tweet:   tweet,
		}
		tweetNo++
	}
	close(jobs)

	// Dequeue the results and save them
	for i := 0; i < len(dictionary); i++ {
		result := <-results
		if result.err == nil {
			log.INFO.Printf("%stransformed tweet no. %d successfully", logPrefix, result.tweetNo)
			if _, ok := userTweetTimes[result.developer.ID]; !ok {
				userTweetTimes[result.developer.ID] = make([]time.Time, 0)
			}
			userTweetTimes[result.developer.ID] = append(userTweetTimes[result.developer.ID], result.tweetCreatedAt)
			// At this point we only save the developer and the game. We still need to aggregate all the possible
			// developerSnapshots
			if _, ok := developerSnapshots[result.developer.ID]; !ok {
				// Create/update the developer if a developer snapshot for it hasn't been added yet
				log.INFO.Printf("%sthis is the first time we have seen developer: %s (%s)", logPrefix, result.developer.Username, result.developer.ID)
				updateOrCreateModel(result.developer)
			}
			// Create the game
			if created := updateOrCreateModel(result.game); created {
				gameIDs.Add(result.game.ID)
			}
			if err != nil {
				return gameIDs, myTwitter.TemporaryWrap(false, err, "could not insert either Developer or Game into DB")
			}
			// Add the developerSnap to the developerSnapshots
			if _, ok := developerSnapshots[result.developer.ID]; !ok {
				developerSnapshots[result.developer.ID] = make([]*models.DeveloperSnapshot, 0)
			}
			developerSnapshots[result.developer.ID] = append(developerSnapshots[result.developer.ID], result.developerSnap)
			log.INFO.Printf("%sadded DeveloperSnapshot to mapping", logPrefix)
		} else {
			log.WARNING.Printf("%scouldn't transform tweet no. %d: %s. Skipping...", logPrefix, result.tweetNo, err.Error())
		}
	}
	return
}

// UpdateDeveloper will fetch the given number of tweets for the models.Developer and run DiscoveryBatch on these tweets
// that will ultimately update the DB row for this models.Developer and create a new models.DeveloperSnapshot for it. It
// will also update any models.Games that exist in the DB for this developer.
func UpdateDeveloper(developerNo int, developer *models.Developer, totalTweets int, userTweetTimes map[string][]time.Time, developerSnapshots map[string][]*models.DeveloperSnapshot) (gameIDs mapset.Set[uuid.UUID], err error) {
	log.INFO.Printf("Updating info for developer %s (%s)", developer.Username, developer.ID)
	var developerSnap *models.DeveloperSnapshot
	if developerSnap, err = developer.LatestDeveloperSnapshot(db.DB); err != nil {
		return gameIDs, myTwitter.TemporaryWrapf(
			true, err, "could not get latest DeveloperSnapshot for %s in UpdateDeveloper",
			developer.ID,
		)
	}
	if developerSnap == nil {
		return gameIDs, myTwitter.TemporaryErrorf(
			true,
			"could not get latest DeveloperSnapshot for %s as there are no DeveloperSnapshots for it",
			developer.ID,
		)
	}
	log.INFO.Printf("Developer %s (%s) latest DeveloperSnapshot is version %d and was created on %s", developer.Username, developer.ID, developerSnap.Version, developerSnap.CreatedAt.String())

	// First we update the user's games that exist in the DB
	var games []*models.Game
	if games, err = developer.Games(db.DB); err != nil {
		return gameIDs, myTwitter.TemporaryWrapf(
			true, err, "could not find Games for Developer %s (%s)", developer.Username, developer.ID,
		)
	}
	log.INFO.Printf("Developer %s (%s) has %d game(s)", developer.Username, developer.ID, len(games))

	for _, game := range games {
		if err = game.Update(db.DB); err != nil {
			log.WARNING.Printf(
				"Could not update Game %s (%s) for Developer %s (%s): %s",
				game.Name.String, game.ID.String(), developer.Username, developer.ID, err.Error(),
			)
			continue
		}
		log.INFO.Printf(
			"Updated Game %s (%s) for Developer %s (%s)",
			game.Name.String, game.ID.String(), developer.Username, developer.ID,
		)
	}

	startTime := developerSnap.LastTweetTime.UTC().Add(time.Second)
	sevenDaysAgo := time.Now().UTC().Add(time.Hour * time.Duration(-24*7))
	// If the startTime is before the last seven days then we will clamp it to seven days ago.
	// Note: This is because RecentSearch can only look through the past 7 days of tweets.
	if startTime.Before(sevenDaysAgo) {
		log.INFO.Printf(
			"Clamping startTime for Developer %s (%s) to %s",
			developer.Username, developer.ID, sevenDaysAgo.Format(time.RFC3339),
		)
		startTime = sevenDaysAgo
	}

	// Construct the query and options for RecentSearch
	query := fmt.Sprintf("%s from:%s", globalConfig.Twitter.TwitterQuery(), developer.ID)
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
		SortOrder:  twitter.TweetSearchSortOrderRecency,
		MaxResults: totalTweets,
		// We start looking for tweets after the last tweet time of the latest developer snapshot for the developer
		StartTime: startTime.UTC().Add(time.Second),
	}

	// Then we make the request for totalTweets from RecentSearch
	log.INFO.Printf(
		"Making RecentSearch request for %d tweets from %s for Developer %s (%s)",
		totalTweets, opts.StartTime.Format(time.RFC3339), developer.Username, developer.ID,
	)
	var result myTwitter.BindingResult
	if result, err = myTwitter.Client.ExecuteBinding(myTwitter.RecentSearch, &myTwitter.BindingOptions{Total: totalTweets}, query, opts); err != nil {
		return gameIDs, errors.Wrapf(
			err, "could not fetch %d tweets for developer %s (%s) after %s",
			totalTweets, developer.Username, developer.ID, opts.StartTime.Format(time.RFC3339),
		)
	}

	// Then we run DiscoveryBatch with this batch of tweets
	tweetRaw := result.Raw().(*twitter.TweetRaw)

	log.INFO.Printf("Executing DiscoveryBatch for batch of %d tweets for developer %s (%s)", result.Meta().ResultCount(), developer.Username, developer.ID)
	// Run DiscoveryBatch for this batch of tweet dictionaries
	if gameIDs, err = DiscoveryBatch(developerNo, tweetRaw.TweetDictionaries(), userTweetTimes, developerSnapshots); err != nil && !myTwitter.IsTemporary(err) {
		log.FATAL.Printf("Error returned by DiscoveryBatch is not temporary: %s. We have to stop :(", err.Error())
		return gameIDs, myTwitter.TemporaryWrapf(
			false, err, "could not execute DiscoveryBatch for tweets fetched for Developer %s (%s)",
			developer.Username, developer.ID,
		)
	}
	return
}

// updateDeveloperJob represents a job that is sent to an updateDeveloperWorker.
type updateDeveloperJob struct {
	developerNo int
	developer   *models.Developer
	totalTweets int
}

// updateDeveloperResult represents a result that is produced by an updateDeveloperWorker.
type updateDeveloperResult struct {
	developerNo        int
	developer          *models.Developer
	userTweetTimes     map[string][]time.Time
	developerSnapshots map[string][]*models.DeveloperSnapshot
	gameIDs            mapset.Set[uuid.UUID]
	error              error
}

// updateDeveloperWorker is a worker for UpdateDeveloper that takes a channel of updateDeveloperJob and queues up the
// result of UpdateDeveloper as an updateDeveloperResult.
func updateDeveloperWorker(jobs <-chan *updateDeveloperJob, results chan<- *updateDeveloperResult) {
	for job := range jobs {
		result := &updateDeveloperResult{
			developerNo:        job.developerNo,
			developer:          job.developer,
			userTweetTimes:     make(map[string][]time.Time),
			developerSnapshots: make(map[string][]*models.DeveloperSnapshot),
		}
		result.gameIDs, result.error = UpdateDeveloper(job.developerNo, job.developer, job.totalTweets, result.userTweetTimes, result.developerSnapshots)
		result.developer = job.developer
		results <- result
	}
}

// sleepBar will sleep for the given time.Duration (as seconds) and display a progress bar for the sleep.
func sleepBar(sleepDuration time.Duration) {
	bar := progressbar.Default(int64(math.Ceil(sleepDuration.Seconds())))
	for s := 0; s < int(math.Ceil(secondsBetweenBatches.Seconds())); s++ {
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
	start := time.Now().UTC()
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
	log.INFO.Printf("Finished Discovery phase in %s", time.Now().UTC().Sub(start).String())

	// 2nd phase: Update. For any developers, and games that exist in the DB but haven't been scraped we will fetch the
	// details from the DB and initiate scrapes for them
	log.INFO.Printf("We have scraped:")
	log.INFO.Printf("\t%d developers", len(developerSnapshots))
	log.INFO.Printf("\t%d games", gameIDs.Cardinality())

	start = time.Now().UTC()
	log.INFO.Printf("Starting Update phase")
	// Find the developers that were not scraped in the discovery phase
	i := 0
	scrapedDevelopers := make([]string, len(developerSnapshots))
	for id := range developerSnapshots {
		scrapedDevelopers[i] = id
		i++
	}

	// SELECT developers.*
	// FROM "developers"
	// INNER JOIN developer_snapshots ON developer_snapshots.developer_id = developers.id
	// WHERE developers.id NOT IN ? AND NOT developers.disabled
	// GROUP BY developers.id
	// HAVING COUNT(developer_snapshots.id) > 0;

	var unscrapedDevelopersQuery *gorm.DB
	if unscrapedDevelopersQuery = db.DB.Select(
		"developers.*",
	).Joins(
		"INNER JOIN developer_snapshots ON developer_snapshots.developer_id = developers.id",
	).Where(
		"developers.id NOT IN ? AND NOT developers.disabled",
		scrapedDevelopers,
	).Group(
		"developers.id",
	).Having(
		"COUNT(developer_snapshots.id) > 0",
	); unscrapedDevelopersQuery.Error != nil {
		return myTwitter.TemporaryWrap(false, unscrapedDevelopersQuery.Error, "cannot construct query for unscraped developers")
	}

	var unscrapedDevelopers []*models.Developer
	if err = unscrapedDevelopersQuery.Model(&models.Developer{}).Find(&unscrapedDevelopers).Error; err != nil {
		log.WARNING.Printf("Cannot fetch developers that were not scraped in the discovery phase: %s", err.Error())
	} else {
		log.INFO.Printf("There are %d developer's that were not scraped in the discovery phase that exist in the DB", len(unscrapedDevelopers))
	}

	if len(unscrapedDevelopers) > 0 {
		// Set up channels for workers
		jobs := make(chan *updateDeveloperJob, len(unscrapedDevelopers))
		results := make(chan *updateDeveloperResult, len(unscrapedDevelopers))

		// For each unscraped developer we will execute a query on RecentSearch for the developer for tweets after the
		// latest developer snapshot's LastTweetTime. We decide how many tweets to request for each developer by dividing
		// the remaining number of tweets for this day by the number of developers we need to update.
		totalTweetsForEachDeveloper := (int(myTwitter.TweetsPerDay) - discoveryTweets) / len(unscrapedDevelopers)
		if totalTweetsForEachDeveloper > maxUpdateTweets {
			totalTweetsForEachDeveloper = maxUpdateTweets
		}
		log.INFO.Printf("Initial number of tweets fetched for %d developers is %d", len(unscrapedDevelopers), totalTweetsForEachDeveloper)

		// In the case that we can't get enough tweets to satisfy the minimum resources per-request for RecentSearch for
		// any developers we will exclude developers one by one based on their latest developer snapshot
		if totalTweetsForEachDeveloper < myTwitter.RecentSearch.Binding().MinResourcesPerRequest {
			log.WARNING.Printf(
				"totalTweetsForEachDeveloper is %d. Reducing the number of developers to see if we can fetch at "+
					"least %d tweet(s) for each developer",
				totalTweetsForEachDeveloper, myTwitter.RecentSearch.Binding().MinResourcesPerRequest,
			)
			initialDeveloperCount := len(unscrapedDevelopers)
			checkDeveloperNo := initialDeveloperCount - 1
			// We iterate until totalTweetsForEachDeveloper is at least the minimum resources per-request for the
			// RecentSearch binding, or checkDeveloper is zero (we cannot make this request as there are too many
			// developers)
			for totalTweetsForEachDeveloper < myTwitter.RecentSearch.Binding().MinResourcesPerRequest && checkDeveloperNo > 0 {
				unscrapedDevelopersQuery.Model(&models.Developer{}).Joins(
					"left join developer_snapshots on developer_snapshots.developer_id = developer.id",
				).Where("developer_snapshots.weighted_score IS NOT NULL").Order("version desc, weighted_score desc").Limit(checkDeveloperNo).Find(&unscrapedDevelopers)
				// We re-calculate totalTweetsForEachDeveloper on each iteration
				totalTweetsForEachDeveloper = (int(myTwitter.TweetsPerDay) - discoveryTweets) / len(unscrapedDevelopers)
				checkDeveloperNo--
			}
			// If we end up with zero developers we will return a non-temporary error
			if checkDeveloperNo == 0 {
				log.FATAL.Printf(
					"Could not get totalTweetsForEachDeveloper up to at least %d, because there is just not "+
						"enough tweetcap left for today (%d rem.) to update %d developers",
					myTwitter.RecentSearch.Binding().MinResourcesPerRequest,
					int(myTwitter.TweetsPerDay)-discoveryTweets,
					initialDeveloperCount,
				)
				return myTwitter.TemporaryErrorf(
					false, "cannot run update phase as we couldn't find a totalTweetsForEachDeveloper "+
						"number that didn't result in 0 unscrapedDevelopers",
				)
			}
			log.INFO.Printf(
				"Found a new totalTweetsForEachDeveloper: %d, by reducing developer count to %d",
				totalTweetsForEachDeveloper, len(unscrapedDevelopers),
			)
		}

		queueDeveloperRange := func(low int, high int) {
			slicedUnscrapedDevelopers := unscrapedDevelopers[low:high]
			currentBatchQueue := 0
			for d, developer := range slicedUnscrapedDevelopers {
				log.INFO.Printf("Queued update for Developer no. %d: %s (%s)", d, developer.Username, developer.ID)
				jobs <- &updateDeveloperJob{
					developerNo: d,
					developer:   developer,
					totalTweets: totalTweetsForEachDeveloper,
				}
				currentBatchQueue++
				// When the current number jobs sent to the updateDeveloperWorkers has reached batchSize, we will wait
				// for a bit before continuing to send them
				if currentBatchQueue == batchSize {
					currentBatchQueue = 0
					log.INFO.Printf(
						"We have queued up %d jobs to the updateDeveloperWorkers so we will take a break of %s",
						batchSize, secondsBetweenBatches.String(),
					)
					sleepBar(secondsBetweenBatches)
				}
			}
		}

		// Start the workers
		for w := 0; w < updateDeveloperWorkers; w++ {
			log.INFO.Printf("Starting updateDeveloperWorker no. %d", w)
			go updateDeveloperWorker(jobs, results)
		}

		// We also check if the rate limit will be exceeded by the number of requests. We use 100 * unscrapedDevelopers
		// as the totalResources arg as we are more interested if the number of requests can be made.
		if err = myTwitter.Client.CheckRateLimit(myTwitter.RecentSearch.Binding(), 100*len(unscrapedDevelopers)); err != nil {
			myTwitter.Client.Mutex.Lock()
			rateLimit, ok := myTwitter.Client.RateLimits[myTwitter.RecentSearch]
			myTwitter.Client.Mutex.Unlock()
			if ok && rateLimit.Reset.Time().After(time.Now().UTC()) {
				log.WARNING.Printf("Due to the number of unscrapedDevelopers (%d) this would exceed the current rate limit of RecentSearch: %d/%d %s. Switching to a batching approach...", rateLimit.Remaining, rateLimit.Limit, rateLimit.Reset.Time().String())
				left := len(unscrapedDevelopers)
				low := 0
				high := rateLimit.Remaining
				for left > 0 {
					log.INFO.Printf("There are %d developers left to queue up for updating", left)
					queueDeveloperRange(low, high)
					sleepDuration := rateLimit.Reset.Time().Sub(time.Now().UTC())
					log.INFO.Printf("We are going to sleep until %s (%s)", rateLimit.Reset.Time().String(), sleepDuration.String())
					sleepBar(sleepDuration)
					myTwitter.Client.Mutex.Lock()
					rateLimit, ok = myTwitter.Client.RateLimits[myTwitter.RecentSearch]
					myTwitter.Client.Mutex.Unlock()
					log.INFO.Printf("I'm awake. New RateLimit?: %t", ok)
					left -= high
					low = high
					if !ok {
						high = myTwitter.RecentSearch.Binding().RequestRateLimit.Requests
					} else {
						high = rateLimit.Remaining
					}
					log.INFO.Printf("Setting low = %d, high = %d", low, high)
				}
			} else {
				return myTwitter.TemporaryWrap(false, err, "do not know how long there is left on the rate limit for RecentSearch as it is stale")
			}
		} else {
			// Otherwise, queue up the jobs for ALL the updateDeveloperWorkers
			queueDeveloperRange(0, len(unscrapedDevelopers))
		}
		close(jobs)

		for r := 0; r < len(unscrapedDevelopers); r++ {
			result := <-results
			// If the error is a RateLimitError, and the error contains info on a twitter.RateLimit then we will sleep
			// until the reset time of this RateLimit.
			var rateLimitError *myTwitter.RateLimitError
			if errors.As(result.error, &rateLimitError) {
				log.WARNING.Printf("RateLimitError occurred in UpdateDeveloper: %s", rateLimitError.Error())
				if rateLimitError.RateLimit != nil {
					sleepDuration := rateLimitError.RateLimit.Reset.Time().Sub(time.Now().UTC())
					log.WARNING.Printf("RateLimitError contains a RateLimit so we will wait %s until it has reset", sleepDuration.String())
					sleepBar(sleepDuration)
				} else {
					log.ERROR.Printf("RateLimitError does not contain a RateLimit instance so we must be exceeding the TweetCap")
					return errors.Wrap(rateLimitError, "rate limit was exceeded in a non-recoverable way")
				}
			} else if result.error != nil && !myTwitter.IsTemporary(result.error) {
				return errors.Wrapf(result.error, "permanent error occurred whilst scraping unscraped developer %s", result.developer.Username)
			} else if result.error != nil {
				log.ERROR.Printf("UpdateDeveloper result for %s (%s) contains an error: %s", result.developer.Username, result.developer.ID, result.error.Error())
				continue
			} else {
				log.INFO.Printf("Got UpdateDeveloper result for %s (%s)", result.developer.Username, result.developer.ID)
			}

			// Merge the gameIDs, userTweetTimes, and developerSnapshots from the result into the maps in this function
			log.INFO.Printf(
				"Merging %d gameIDs from the result for %s (%s), back into gameIDs",
				result.gameIDs.Cardinality(), result.developer.Username, result.developer.ID,
			)
			gameIDs = gameIDs.Union(result.gameIDs)

			log.INFO.Printf(
				"Merging %d userTweetTimes from the result for %s (%s), back into userTweetTimes",
				len(result.userTweetTimes), result.developer.Username, result.developer.ID,
			)
			for id, tweetTimes := range result.userTweetTimes {
				if _, ok := userTweetTimes[id]; !ok {
					// If the key doesn't exist then we have to make a new array at that key, but we can use the copy
					// function to just copy the tweetTimes to the empty array
					userTweetTimes[id] = make([]time.Time, len(tweetTimes))
					copy(userTweetTimes[id], tweetTimes)
				} else {
					userTweetTimes[id] = append(userTweetTimes[id], tweetTimes...)
				}
			}

			log.INFO.Printf(
				"Merging %d developerSnapshots from the result for %s (%s), back into developerSnapshots",
				len(result.developerSnapshots), result.developer.Username, result.developer.ID,
			)
			for id, developerSnaps := range result.developerSnapshots {
				if _, ok := developerSnapshots[id]; !ok {
					developerSnapshots[id] = make([]*models.DeveloperSnapshot, len(developerSnaps))
					copy(developerSnapshots[id], developerSnaps)
				} else {
					for _, developerSnap := range developerSnaps {
						developerSnapshots[id] = append(developerSnapshots[id], developerSnap)
					}
				}
			}
		}
	}
	log.INFO.Printf("Finished Update phase in %s", time.Now().UTC().Sub(start).String())

	// 3rd phase: Snapshot. For all the partial DeveloperSnapshots that exist in the developerSnapshots map we will
	// aggregate the information for them.
	start = time.Now().UTC()
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
	log.INFO.Printf("Finished aggregating DeveloperSnapshots in %s", time.Now().UTC().Sub(start).String())

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

	// 5th phase: Measure. For all the users that have a good number of snapshots we'll see if any are shining above the
	// rest
	return
}
