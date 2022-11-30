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
	"reflect"
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

// TransformTweet takes a twitter.TweetDictionary and splits it out into instances of the models.Developer,
// models.DeveloperSnapshot, and models.Game models. It also returns the time when the tweet was created so that we can
// track the duration between tweets for models.DeveloperSnapshot.
func TransformTweet(
	tweet *twitter.TweetDictionary,
	gameScrapers *models.StorefrontScrapers[string],
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

	// Continue we find if there are any SteamURLAppPages that are linked in the tweet
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
					storefrontMap[check.storefront] = mapset.NewThreadUnsafeSet[string]()
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
		gameModel := <-gameScrapers.Add(false, &models.Game{Developer: developer}, storefrontMap)
		game = gameModel.(*models.Game)
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
	gameScrapers *models.StorefrontScrapers[string],
) {
	for job := range jobs {
		result := &transformTweetResult{}
		result.developer, result.developerSnap, result.game, result.tweetCreatedAt, result.err = TransformTweet(
			job.tweet,
			gameScrapers,
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
	gameScrapers *models.StorefrontScrapers[string],
	state *ScoutState,
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
	gameIDs = mapset.NewThreadUnsafeSet[uuid.UUID]()

	// Start the transformTweetWorkers
	for i := 0; i < transformTweetWorkers; i++ {
		go transformTweetWorker(jobs, results, gameScrapers)
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
			state.GetIterableCachedField(UserTweetTimesType).SetOrAdd(result.developer.ID, result.tweetCreatedAt)

			// At this point we only save the developer and the game. We still need to aggregate all the possible
			// developerSnapshots
			if _, ok := state.GetIterableCachedField(DeveloperSnapshotsType).Get(result.developer.ID); !ok {
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
			state.GetIterableCachedField(DeveloperSnapshotsType).SetOrAdd(result.developer.ID, result.developerSnap)
			log.INFO.Printf("%sadded DeveloperSnapshot to mapping", logPrefix)
		} else {
			log.WARNING.Printf("%scouldn't transform tweet no. %d: %s. Skipping...", logPrefix, result.tweetNo, err.Error())
		}
	}
	return
}

// DiscoveryPhase executes batches of DiscoveryBatch sequentially and fills out the userTweetTimes, and
// developerSnapshots maps with the data returned from each DiscoveryBatch.
func DiscoveryPhase(state *ScoutState) (gameIDs mapset.Set[uuid.UUID], err error) {
	// Set the batchSize and discoveryTweets vars by reading from ScoutState
	stateState := state.GetCachedField(StateType).(*State)
	batchSize := stateState.BatchSize
	discoveryTweets := stateState.DiscoveryTweets
	batchNo := stateState.CurrentDiscoveryBatch

	// Search the recent tweets on Twitter belonging to the hashtags given in config.json.
	gameIDs = mapset.NewThreadUnsafeSet[uuid.UUID]()
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

	// If we are continuing from a loaded state then we check if there is a token to continue the RecentSearch batches
	// from
	if state.Loaded && stateState.CurrentDiscoveryToken != "" {
		opts.NextToken = stateState.CurrentDiscoveryToken
	}

	// Start the workers for scraping games.
	gameScrapers := models.NewStorefrontScrapers[string](
		globalConfig.Scrape, db.DB,
		discoveryGameScrapeWorkers, discoveryMaxConcurrentGameScrapeWorkers, batchSize*maxGamesPerTweet,
		minScrapeStorefrontsForGameWorkerWaitTime, maxScrapeStorefrontsForGameWorkerWaitTime,
	)
	gameScrapers.Start()

	log.INFO.Println("Starting Discovery phase")
	for i := batchNo * batchSize; i < discoveryTweets; i += batchSize {
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
			gameScrapers,
			state,
		); err != nil && !myErrors.IsTemporary(err) {
			log.FATAL.Printf("Error returned by DiscoveryBatch is not temporary: %s. We have to stop :(", err.Error())
			err = errors.Wrapf(err, "could not execute DiscoveryBatch for batch no. %d", batchNo)
			return
		}
		gameIDs = gameIDs.Union(subGameIDs)

		// Set up the next batch of requests by finding the NextToken of the current batch
		opts.NextToken = result.Meta().NextToken()
		log.INFO.Printf(
			"Set NextToken for next batch (%d) to %s by getting the NextToken from the previous result",
			batchNo, opts.NextToken,
		)
		batchNo++

		// Set the fields in the State which indicate the current batch info
		state.GetCachedField(StateType).SetOrAdd("CurrentDiscoveryBatch", batchNo)
		state.GetCachedField(StateType).SetOrAdd("CurrentDiscoveryToken", opts.NextToken)

		// Save the ScoutState after each batch
		if err = state.Save(); err != nil {
			log.ERROR.Printf("Could not save ScoutState: %v", err)
		}

		// Nap time
		log.INFO.Printf(
			"Continue batch in %s. Sleeping for %s so we don't overdo it",
			time.Now().UTC().Sub(batchStart).String(), secondsBetweenDiscoveryBatches.String(),
		)
		sleepBar(secondsBetweenDiscoveryBatches)
	}

	gameScrapers.Wait()
	log.INFO.Println("StorefrontScrapers have finished")

	return
}
