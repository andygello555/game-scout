package main

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myErrors "github.com/andygello555/game-scout/errors"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/rand"
	"reflect"
	"sync"
	"time"
)

const (
	// minScrapeStorefrontsForGameWorkerWaitTime is the minimum amount of time for a scrapeStorefrontsForGameWorker to
	// wait after completing a job.
	minScrapeStorefrontsForGameWorkerWaitTime = time.Millisecond * 100
	// maxScrapeStorefrontsForGameWorkerWaitTime is the maximum amount of time for a scrapeStorefrontsForGameWorker to
	// wait after completing a job.
	maxScrapeStorefrontsForGameWorkerWaitTime = time.Millisecond * 500
	// maxGamesPerTweet is needed so that we don't overload the queue to the scrapeStorefrontsForGameWorker.
	maxGamesPerTweet = 10
)

// scrapeStorefrontsForGameJob is a unit of work for the scrapeStorefrontsForGameWorker.
type scrapeStorefrontsForGameJob struct {
	// game is the game to fill/update.
	game *models.Game
	// update represents whether to call game.Update instead of models.ScrapeStorefrontsForGame.
	update         bool
	storefrontURLs map[models.Storefront]mapset.Set[string]
	// result is a channel into which the resulting models.Game will be pushed once the job has finished. This is so that
	// the producer of the job can wait until it has finished.
	result chan *models.Game
}

// scrapeStorefrontsForGameWorker will execute the channel of jobs as arguments to the models.ScrapeStorefrontsForGame
// function. This worker shouldn't be started more than 2 times as its entire purpose is to throttle the rate at which
// games are scraped to make sure that we don't get rate limited. In fact, the guard parameter is given so that no more
// than maxConcurrentScrapeStorefrontsForGameWorkers is running at once.
func scrapeStorefrontsForGameWorker(
	no int,
	wg *sync.WaitGroup,
	jobs <-chan *scrapeStorefrontsForGameJob,
	guard chan struct{},
) {
	defer wg.Done()
	time.Sleep(maxScrapeStorefrontsForGameWorkerWaitTime * time.Duration(no))
	log.INFO.Printf("ScrapeStorefrontsForGameWorker no. %d has started...", no)
	jobTotal := 0
	for job := range jobs {
		guard <- struct{}{}
		log.INFO.Printf(
			"ScrapeStorefrontsForGameWorker no. %d has acquired the guard, and is now executing a job marked as "+
				"update = %t",
			no, job.update,
		)

		if job.update {
			if err := job.game.Update(db.DB, globalConfig.Scrape); err != nil {
				log.WARNING.Printf(
					"ScrapeStorefrontsForGameWorker no. %d could not save game %s: %s",
					no, job.game.ID.String(), err.Error(),
				)
			}
		} else {
			models.ScrapeStorefrontsForGame(&job.game, job.storefrontURLs, globalConfig.Scrape)
		}
		job.result <- job.game
		jobTotal++

		// Find out how long to sleep for...
		min := int64(minScrapeStorefrontsForGameWorkerWaitTime)
		max := int64(maxScrapeStorefrontsForGameWorkerWaitTime)
		random := rand.Int63n(max-min+1) + min
		sleepDuration := time.Duration(random)
		log.INFO.Printf(
			"ScrapeStorefrontsForGameWorker no. %d has completed a job and is now sleeping for %s",
			no, sleepDuration.String(),
		)
		time.Sleep(sleepDuration)
		<-guard
	}
	log.INFO.Printf("ScrapeStorefrontsForGameWorker no. %d has finished. Executed %d jobs", no, jobTotal)
}

// TransformTweet takes a twitter.TweetDictionary and splits it out into instances of the models.Developer,
// models.DeveloperSnapshot, and models.Game models. It also returns the time when the tweet was created so that we can
// track the duration between tweets for models.DeveloperSnapshot.
func TransformTweet(
	tweet *twitter.TweetDictionary,
	gameScrapeQueue chan<- *scrapeStorefrontsForGameJob,
) (
	developer *models.Developer,
	developerSnap *models.DeveloperSnapshot,
	game *models.Game,
	tweetCreatedAt time.Time,
	err error,
) {
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

	developerSnap.TweetsPublicMetrics = tweet.Tweet.PublicMetrics
	developerSnap.UserPublicMetrics = developer.PublicMetrics
	developerSnap.ContextAnnotationSet = myTwitter.NewContextAnnotationSet(tweet.Tweet.ContextAnnotations...)

	// Next we find if there are any SteamURLAppPages that are linked in the tweet
	storefrontMap := make(map[models.Storefront]mapset.Set[string])
	totalGames := 0
out:
	for _, url := range tweet.Tweet.Entities.URLs {
		for _, check := range []struct {
			url        browser.ScrapeURL
			storefront models.Storefront
		}{
			{browser.SteamAppPage, models.SteamStorefront},
		} {
			// If the expanded URL is in the same format as the ScrapeURL
			if check.url.Match(url.ExpandedURL) {
				if _, ok := storefrontMap[check.storefront]; !ok {
					storefrontMap[check.storefront] = mapset.NewSet[string]()
				}
				if !storefrontMap[check.storefront].Contains(url.ExpandedURL) {
					storefrontMap[check.storefront].Add(url.ExpandedURL)
					// Because we know that this is a new game, we will increment the game total
					totalGames++
				}
			}

			// If we have reached the maximum number of games, then we will exit out of these loops
			if totalGames == maxGamesPerTweet {
				log.WARNING.Printf(
					"We have reached the maximum number of games found in tweet: %s, for author %s (%s)",
					tweet.Tweet.ID, developer.Username, developer.ID,
				)
				break out
			}
		}
	}

	// Scrape metrics from all the storefronts found for the game. We set the Developer field of the Game so that we can
	// match any username's found on the Game's website. We only do this if there are Storefronts in the storefrontMap,
	// otherwise we'll end up waiting for nothing
	game = nil
	if len(storefrontMap) > 0 {
		scrapeGameDone := make(chan *models.Game)
		gameScrapeQueue <- &scrapeStorefrontsForGameJob{
			game:           &models.Game{Developer: developer},
			update:         false,
			storefrontURLs: storefrontMap,
			result:         scrapeGameDone,
		}
		game = <-scrapeGameDone
		if game != nil {
			game.DeveloperID = developer.ID
		}
	}
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
func transformTweetWorker(
	jobs <-chan *transformTweetJob,
	results chan<- *transformTweetResult,
	gameScrapeQueue chan *scrapeStorefrontsForGameJob,
) {
	for job := range jobs {
		result := &transformTweetResult{}
		result.developer, result.developerSnap, result.game, result.tweetCreatedAt, result.err = TransformTweet(
			job.tweet,
			gameScrapeQueue,
		)
		result.tweetNo = job.tweetNo
		results <- result
	}
}

// DiscoveryBatch takes a twitter.TweetDictionary and runs TransformTweet on each tweet that is within it. It also takes
// references to userTweetTimes and developerSnapshots, which are passed in from either Scout, or UpdateDeveloper. Due
// to the rate limiting by Steam on their store API, you can also pass in how many gameWorkers (a worker which scrapes a
// game) to start, and how many of these game workers can be executing at once.
//
// The gameScrapeQueue is the channel to which games are queued to be scraped. This channel, along with the workers,
// should be created and close outside DiscoveryBatch. If DiscoveryBatch is being run in the discovery phase, then it
// will be created in Scout. Otherwise, if it is running in UpdateDeveloper, they should be created in UpdatePhase.
func DiscoveryBatch(
	batchNo int,
	dictionary map[string]*twitter.TweetDictionary,
	gameScrapeQueue chan *scrapeStorefrontsForGameJob,
	userTweetTimes map[string][]time.Time,
	developerSnapshots map[string][]*models.DeveloperSnapshot,
) (gameIDs mapset.Set[uuid.UUID], err error) {
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
			OnCreateOmit() []string
		}
		// Then we construct the upsert clause
		onConflict := value.(upsertable).OnConflict()
		onConflict.DoUpdates = clause.AssignmentColumns(db.GetModel(value).ColumnDBNamesExcluding("id"))
		if updateOrCreate = db.DB.Clauses(onConflict); updateOrCreate.Error != nil {
			err = myErrors.TemporaryWrapf(
				false,
				updateOrCreate.Error,
				"could not create update or create clause for %s",
				reflect.TypeOf(value).Elem().Name(),
			)
			return
		}

		// Finally, we create the model instance, omitting any fields that should be omitted
		if after := updateOrCreate.Omit(value.(upsertable).OnCreateOmit()...).Create(value); after.Error != nil {
			err = after.Error
			return
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

	// Start the transformTweetWorkers
	for i := 0; i < transformTweetWorkers; i++ {
		go transformTweetWorker(jobs, results, gameScrapeQueue)
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
				return gameIDs, myErrors.TemporaryWrap(false, err, "could not insert either Developer or Game into DB")
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

// DiscoveryPhase executes batches of DiscoveryBatch sequentially and fills out the userTweetTimes, and
// developerSnapshots maps with the data returned from each DiscoveryBatch.
func DiscoveryPhase(
	batchSize int,
	discoveryTweets int,
	userTweetTimes map[string][]time.Time,
	developerSnapshots map[string][]*models.DeveloperSnapshot,
) (gameIDs mapset.Set[uuid.UUID], err error) {
	// Search the recent tweets on Twitter belonging to the hashtags given in config.json.
	gameIDs = mapset.NewSet[uuid.UUID]()
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

	// Start the workers for scraping games.
	// Note: in the discovery phase we can usually safely start a few more workers than in update as we are fetching
	//       batches sequentially.
	gameScrapeQueue := make(chan *scrapeStorefrontsForGameJob, batchSize*maxGamesPerTweet)
	gameScrapeGuard := make(chan struct{}, discoveryMaxConcurrentGameScrapeWorkers)
	var gameScrapeWg sync.WaitGroup

	// Start the scrapeStorefrontsForGameWorkers
	for i := 0; i < discoveryGameScrapeWorkers; i++ {
		gameScrapeWg.Add(1)
		go scrapeStorefrontsForGameWorker(i+1, &gameScrapeWg, gameScrapeQueue, gameScrapeGuard)
	}

	batchNo := 1
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
		if result, err = myTwitter.Client.ExecuteBinding(
			myTwitter.RecentSearch,
			&myTwitter.BindingOptions{Total: offset},
			query, opts,
		); err != nil {
			log.ERROR.Printf("Could not fetch %d tweets in Scout on batch %d/%d: %s. Continuing...", offset, i, discoveryTweets, err.Error())
			continue
		}

		// The sub-phase of the Discovery phase is the cleansing phase
		tweetRaw := result.Raw().(*twitter.TweetRaw)

		log.INFO.Printf("Executing DiscoveryBatch for batch no. %d (%d/%d) for %d tweets", batchNo, i, discoveryTweets, len(tweetRaw.TweetDictionaries()))
		// Run DiscoveryBatch for this batch of tweet dictionaries
		// If the error is not temporary then we will kill the Scouting process
		var subGameIDs mapset.Set[uuid.UUID]
		if subGameIDs, err = DiscoveryBatch(
			batchNo,
			tweetRaw.TweetDictionaries(),
			gameScrapeQueue,
			userTweetTimes,
			developerSnapshots,
		); err != nil && !myErrors.IsTemporary(err) {
			log.FATAL.Printf("Error returned by DiscoveryBatch is not temporary: %s. We have to stop :(", err.Error())
			err = errors.Wrapf(err, "could not execute DiscoveryBatch for batch no. %d", batchNo)
			return
		}
		gameIDs = gameIDs.Union(subGameIDs)

		// Set up the next batch of requests by finding the NextToken of the current batch
		opts.NextToken = result.Meta().NextToken()
		batchNo++
		log.INFO.Printf("Set NextToken for next batch (%d) to %s by getting the NextToken from the previous result", batchNo, opts.NextToken)

		// Nap time
		log.INFO.Printf(
			"Finished batch in %s. Sleeping for %s so we don't overdo it",
			time.Now().UTC().Sub(batchStart).String(), secondsBetweenDiscoveryBatches.String(),
		)
		sleepBar(secondsBetweenDiscoveryBatches)
	}

	close(gameScrapeQueue)
	log.INFO.Printf("Closed gameScrapeQueue")
	gameScrapeWg.Wait()
	log.INFO.Printf("Game scrape workers have all stopped")

	return
}
