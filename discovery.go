package main

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/rand"
	"reflect"
	"time"
)

const (
	// minScrapeStorefrontsForGameWorkerWaitTime is the minimum amount of time for a scrapeStorefrontsForGameWorker to
	// wait after completing a job.
	minScrapeStorefrontsForGameWorkerWaitTime = time.Second * 3
	// maxScrapeStorefrontsForGameWorkerWaitTime is the maximum amount of time for a scrapeStorefrontsForGameWorker to
	// wait after completing a job.
	maxScrapeStorefrontsForGameWorkerWaitTime = time.Second * 10
	// maxGamesPerTweet is needed so that we don't overload the queue to the scrapeStorefrontsForGameWorker.
	maxGamesPerTweet = 10
	// scrapeStorefrontsForGameWorkers is the number of scrapeStorefrontsForGameWorker to start in the DiscoveryBatch
	// function.
	scrapeStorefrontsForGameWorkers = 2
	// maxConcurrentScrapeStorefrontsForGameWorkers is the maximum number of scrapeStorefrontsForGameWorker that can be
	// running at the same time.
	maxConcurrentScrapeStorefrontsForGameWorkers = 2
)

// scrapeStorefrontsForGameJob is a unit of work for the scrapeStorefrontsForGameWorker.
type scrapeStorefrontsForGameJob struct {
	game           *models.Game
	storefrontURLs map[models.Storefront]mapset.Set[string]
	// result is a channel into which the resulting models.Game will be pushed once the job has finished. This is so that
	// the producer of the job can wait until it has finished.
	result chan *models.Game
}

// scrapeStorefrontsForGameWorker will execute the channel of jobs as arguments to the models.ScrapeStorefrontsForGame
// function. This worker shouldn't be started more than 2 times as its entire purpose is to throttle the rate at which
// games are scraped to make sure that we don't get rate limited. In fact, the guard parameter is given so that no more
// than maxConcurrentScrapeStorefrontsForGameWorkers is running at once.
func scrapeStorefrontsForGameWorker(no int, jobs <-chan *scrapeStorefrontsForGameJob, guard chan struct{}) {
	log.INFO.Printf("ScrapeStorefrontsForGameWorker no. %d has started...", no)
	for job := range jobs {
		guard <- struct{}{}
		models.ScrapeStorefrontsForGame(&job.game, job.storefrontURLs)
		job.result <- job.game
		min := int64(minScrapeStorefrontsForGameWorkerWaitTime)
		max := int64(maxScrapeStorefrontsForGameWorkerWaitTime)
		random := rand.Int63n(max-min+1) + min
		sleepDuration := time.Duration(random)
		log.INFO.Printf(
			"ScrapeStorefrontsForGameWorker no. %d has completed a job and is now sleeping for %s",
			sleepDuration.String(),
		)
		time.Sleep(sleepDuration)
		<-guard
	}
}

// TransformTweet takes a twitter.TweetDictionary and splits it out into instances of the models.Developer,
// models.DeveloperSnapshot, and models.Game models. It also returns the time when the tweet was created so that we can
// track the duration between tweets for models.DeveloperSnapshot.
func TransformTweet(tweet *twitter.TweetDictionary, gameScrapeQueue chan<- *scrapeStorefrontsForGameJob) (developer *models.Developer, developerSnap *models.DeveloperSnapshot, game *models.Game, tweetCreatedAt time.Time, err error) {
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
	// match any username's found on the Game's website.
	//game = &models.Game{Developer: developer}
	//models.ScrapeStorefrontsForGame(&game, storefrontMap)
	//if game != nil {
	//	game.DeveloperID = developer.ID
	//}
	scrapeGameDone := make(chan *models.Game)
	gameScrapeQueue <- &scrapeStorefrontsForGameJob{
		game:           &models.Game{Developer: developer},
		storefrontURLs: storefrontMap,
		result:         scrapeGameDone,
	}
	game = <-scrapeGameDone
	if game != nil {
		game.DeveloperID = developer.ID
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
func transformTweetWorker(jobs <-chan *transformTweetJob, results chan<- *transformTweetResult, gameScrapeQueue chan *scrapeStorefrontsForGameJob) {
	for job := range jobs {
		result := &transformTweetResult{}
		result.developer, result.developerSnap, result.game, result.tweetCreatedAt, result.err = TransformTweet(job.tweet, gameScrapeQueue)
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
			OnCreateOmit() []string
		}
		// Then we construct the upsert clause
		onConflict := value.(upsertable).OnConflict()
		onConflict.DoUpdates = clause.AssignmentColumns(db.GetModel(value).ColumnDBNamesExcluding("id"))
		if updateOrCreate = db.DB.Clauses(onConflict); updateOrCreate.Error != nil {
			err = myTwitter.TemporaryWrapf(
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
		}
		log.INFO.Printf("%screated %s", logPrefix, reflect.TypeOf(value).Elem().Name())
		created = true
		return
	}

	log.INFO.Printf("Starting DiscoveryBatch no. %d", batchNo)

	// Create the job and result channels for the transformTweetWorkers
	jobs := make(chan *transformTweetJob, len(dictionary))
	results := make(chan *transformTweetResult, len(dictionary))
	gameScrapeQueue := make(chan *scrapeStorefrontsForGameJob, len(dictionary)*maxGamesPerTweet)
	gameScrapeGuard := make(chan struct{}, maxConcurrentScrapeStorefrontsForGameWorkers)
	gameIDs = mapset.NewSet[uuid.UUID]()

	// Start the scrapeStorefrontsForGameWorkers
	for i := 0; i < scrapeStorefrontsForGameWorkers; i++ {
		go scrapeStorefrontsForGameWorker(i+1, gameScrapeQueue, gameScrapeGuard)
	}

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
	close(gameScrapeQueue)
	return
}
