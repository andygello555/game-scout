package main

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"sync"
	"time"
)

// UpdateDeveloper will fetch the given number of tweets for the models.Developer and run DiscoveryBatch on these tweets
// that will ultimately update the DB row for this models.Developer and create a new models.DeveloperSnapshot for it. It
// will also update any models.Games that exist in the DB for this developer.
func UpdateDeveloper(
	developerNo int,
	developer *models.Developer,
	totalTweets int,
	gameScrapeQueue chan *scrapeStorefrontsForGameJob,
	userTweetTimes map[string][]time.Time,
	developerSnapshots map[string][]*models.DeveloperSnapshot,
) (gameIDs mapset.Set[uuid.UUID], err error) {
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
	gameIDs = mapset.NewSet[uuid.UUID]()
	if games, err = developer.Games(db.DB); err != nil {
		return gameIDs, myTwitter.TemporaryWrapf(
			true, err, "could not find Games for Developer %s (%s)", developer.Username, developer.ID,
		)
	}
	log.INFO.Printf("Developer %s (%s) has %d game(s)", developer.Username, developer.ID, len(games))

	// Update all the games. We just start a new goroutine for each game, so we don't block this goroutine and can
	// continue onto fetching the set of tweets.
	for _, game := range games {
		gameIDs.Add(game.ID)
		go func(game *models.Game) {
			log.INFO.Printf(
				"Queued update for Game \"%s\" (%s) for Developer %s (%s)",
				game.Name.String, game.ID.String(), developer.Username, developer.ID,
			)
			game.Developer = developer
			scrapeGameDone := make(chan *models.Game)
			gameScrapeQueue <- &scrapeStorefrontsForGameJob{
				game:   game,
				update: true,
				result: scrapeGameDone,
			}
			<-scrapeGameDone
			log.INFO.Printf(
				"Updated Game \"%s\" (%s) for Developer %s (%s)",
				game.Name.String, game.ID.String(), developer.Username, developer.ID,
			)
		}(game)
	}

	startTime := developerSnap.LastTweetTime.UTC().Add(time.Second)
	sevenDaysAgo := time.Now().UTC().Add(time.Hour * time.Duration(-24*7))
	// If the startTime is before the last seven days then we will clamp it to seven days ago.
	// Note: This is because RecentSearch can only look through the past 7 days of tweets.
	if startTime.Before(sevenDaysAgo) {
		log.WARNING.Printf(
			"Clamping startTime for Developer %s (%s) to %s",
			developer.Username, developer.ID, sevenDaysAgo.Format(time.RFC3339),
		)
		startTime = sevenDaysAgo
	}

	// Construct the query and options for RecentSearch
	// The query looks for tweets containing the hashtags supplied in config.json that are from the user of the given
	// developer.ID and are not retweets.
	query := fmt.Sprintf("%s from:%s -is:retweet", globalConfig.Twitter.TwitterQuery(), developer.ID)
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
		log.ERROR.Printf(
			"Could not fetch %d tweets for developer %s (%s) after %s",
			totalTweets, developer.Username, developer.ID, opts.StartTime.Format(time.RFC3339),
		)
		return gameIDs, errors.Wrapf(
			err, "could not fetch %d tweets for developer %s (%s) after %s",
			totalTweets, developer.Username, developer.ID, opts.StartTime.Format(time.RFC3339),
		)
	}

	// Then we run DiscoveryBatch with this batch of tweets
	tweetRaw := result.Raw().(*twitter.TweetRaw)

	log.INFO.Printf(
		"Executing DiscoveryBatch for batch of %d tweets for developer %s (%s)",
		result.Meta().ResultCount(), developer.Username, developer.ID,
	)
	// Run DiscoveryBatch for this batch of tweet dictionaries
	var subGameIDs mapset.Set[uuid.UUID]
	if subGameIDs, err = DiscoveryBatch(
		developerNo, tweetRaw.TweetDictionaries(), gameScrapeQueue, userTweetTimes, developerSnapshots,
	); err != nil && !myTwitter.IsTemporary(err) {
		log.FATAL.Printf("Error returned by DiscoveryBatch is not temporary: %s. We have to stop :(", err.Error())
		return gameIDs, myTwitter.TemporaryWrapf(
			false, err, "could not execute DiscoveryBatch for tweets fetched for Developer %s (%s)",
			developer.Username, developer.ID,
		)
	}
	// Merge the gameIDs from DiscoveryBatch back into the local gameIDs
	gameIDs = gameIDs.Union(subGameIDs)
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
func updateDeveloperWorker(
	wg *sync.WaitGroup,
	jobs <-chan *updateDeveloperJob,
	results chan<- *updateDeveloperResult,
	gameScrapeQueue chan *scrapeStorefrontsForGameJob,
) {
	defer wg.Done()
	for job := range jobs {
		result := &updateDeveloperResult{
			developerNo:        job.developerNo,
			developer:          job.developer,
			userTweetTimes:     make(map[string][]time.Time),
			developerSnapshots: make(map[string][]*models.DeveloperSnapshot),
		}
		result.gameIDs, result.error = UpdateDeveloper(
			job.developerNo, job.developer, job.totalTweets,
			gameScrapeQueue, result.userTweetTimes, result.developerSnapshots,
		)
		result.developer = job.developer
		results <- result
	}
}

// UpdatePhase will update the developers with the given IDs. It will populate the given maps/sets for userTweetTimes,
// developerSnapshots, and gameIDs. Internally, a goroutine/multiple goroutines will be created for each of the
// following:
//
// • Workers (no = updateDeveloperWorkers): processes updateDeveloperJobs in batches. In these workers the RecentSearch
// binding will be executed, meaning that we have to manage the rate limit correctly. Once a developer has been updated,
// the result will be pushed as an updateDeveloperResult into a result channel to be processed by the consumer.
//
// • Producer (no = 1): the producer queues up updateDeveloperJobs to be processed by the workers. This is done in a way
// that will not exceed the rate limit for the RecentSearch binding. Internally, this works by queueing up X number of
// jobs where X is equal to the remaining requests on the most recent rate limit for RecentSearch. Once it has done
// this, it will wait until the rate limit has reset and the jobs have been processed. After fetching the most recent
// rate limit for RecentSearch, it will repeat the process for the remaining jobs.
//
// • Consumer (no = 1): dequeues results from the results channel and aggregates them into the userTweetTimes,
// developerSnapshots, and gameIDs instances. Once a result has been processed, the developerNo of the
// updateDeveloperResult will be pushed to a buffered channel to notify the producer that that job has been processed.
func UpdatePhase(
	batchSize int,
	discoveryTweets int,
	developerIDs []string,
	userTweetTimes map[string][]time.Time,
	developerSnapshots map[string][]*models.DeveloperSnapshot,
) (gameIDs mapset.Set[uuid.UUID], err error) {
	gameIDs = mapset.NewSet[uuid.UUID]()
	// SELECT developers.*
	// FROM "developers"
	// INNER JOIN developer_snapshots ON developer_snapshots.developer_id = developers.id
	// WHERE developers.id NOT IN ? AND NOT developers.disabled
	// GROUP BY developers.id
	// HAVING COUNT(developer_snapshots.id) > 0;

	var unscrapedDevelopersQuery *gorm.DB
	if len(developerIDs) > 0 {
		unscrapedDevelopersQuery = db.DB.Select(
			"developers.*",
		).Joins(
			"INNER JOIN developer_snapshots ON developer_snapshots.developer_id = developers.id",
		).Where(
			"developers.id NOT IN ? AND NOT developers.disabled",
			developerIDs,
		).Group(
			"developers.id",
		).Having(
			"COUNT(developer_snapshots.id) > 0",
		)
	} else {
		unscrapedDevelopersQuery = db.DB.Select(
			"developers.*",
		).Joins(
			"INNER JOIN developer_snapshots ON developer_snapshots.developer_id = developers.id",
		).Where("NOT developers.disabled").Group(
			"developers.id",
		).Having(
			"COUNT(developer_snapshots.id) > 0",
		)
	}

	if unscrapedDevelopersQuery.Error != nil {
		err = myTwitter.TemporaryWrap(false, unscrapedDevelopersQuery.Error, "cannot construct query for unscraped developers")
		return
	}

	var unscrapedDevelopers []*models.Developer
	if err = unscrapedDevelopersQuery.Model(&models.Developer{}).Find(&unscrapedDevelopers).Error; err != nil {
		log.WARNING.Printf("Cannot fetch developers that were not scraped in the discovery phase: %s", err.Error())
	} else {
		log.INFO.Printf("There are %d developer's that were not scraped in the discovery phase that exist in the DB", len(unscrapedDevelopers))
	}

	if len(unscrapedDevelopers) > 0 {
		// Set up channels, wait group, and sync.Once for pipeline.
		jobs := make(chan *updateDeveloperJob, len(unscrapedDevelopers))
		results := make(chan *updateDeveloperResult, len(unscrapedDevelopers))
		// Synchronizing worker goroutines
		var updateDeveloperWorkerWg sync.WaitGroup
		// Indicates when the producer has finished
		producerDone := make(chan struct{})
		// Indicated when the consumer has finished (as well as with what error)
		consumerDone := make(chan error)
		// Indicates which jobs have finished in the current producer batch
		finishedJobs := make(chan int, len(unscrapedDevelopers))

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
				err = myTwitter.TemporaryErrorf(
					false, "cannot run update phase as we couldn't find a totalTweetsForEachDeveloper "+
						"number that didn't result in 0 unscrapedDevelopers",
				)
				return
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
						batchSize, secondsBetweenUpdateBatches.String(),
					)
					sleepBar(secondsBetweenUpdateBatches)
				}
			}
		}

		// Start the workers for scraping games.
		// Note: in the update phase we have to be a bit more cautious with the number of workers that we start. This is
		//       because each developer in a batch is updated in parallel.
		gameScrapeQueue := make(
			chan *scrapeStorefrontsForGameJob,
			len(unscrapedDevelopers)*totalTweetsForEachDeveloper*maxGamesPerTweet,
		)
		gameScrapeGuard := make(chan struct{}, updateMaxConcurrentGameScrapeWorkers)
		var gameScrapeWg sync.WaitGroup

		// Start the scrapeStorefrontsForGameWorkers
		for i := 0; i < updateGameScrapeWorkers; i++ {
			gameScrapeWg.Add(1)
			go scrapeStorefrontsForGameWorker(i+1, &gameScrapeWg, gameScrapeQueue, gameScrapeGuard)
		}

		// Start the workers
		for w := 0; w < updateDeveloperWorkers; w++ {
			log.INFO.Printf("Starting updateDeveloperWorker no. %d", w)
			updateDeveloperWorkerWg.Add(1)
			go updateDeveloperWorker(&updateDeveloperWorkerWg, jobs, results, gameScrapeQueue)
		}

		// We also check if the rate limit will be exceeded by the number of requests. We use 100 * unscrapedDevelopers
		// as the totalResources arg as we are more interested if the number of requests can be made.
		if err = myTwitter.Client.CheckRateLimit(myTwitter.RecentSearch.Binding(), 100*len(unscrapedDevelopers)); err != nil {
			log.WARNING.Printf(
				"%d requests for %d developers will exceed our rate limit. We need to check if we can batch...",
				len(unscrapedDevelopers), len(unscrapedDevelopers),
			)
			myTwitter.Client.Mutex.Lock()
			rateLimit, ok := myTwitter.Client.RateLimits[myTwitter.RecentSearch]
			myTwitter.Client.Mutex.Unlock()
			log.WARNING.Printf("Managed to get rate limit for RecentSearch: %v", rateLimit)
			if ok && rateLimit.Reset.Time().After(time.Now().UTC()) {
				log.WARNING.Printf(
					"Due to the number of unscrapedDevelopers (%d) this would exceed the current rate limit of "+
						"RecentSearch: %d/%d %s. Switching to a batching approach...",
					len(unscrapedDevelopers), rateLimit.Remaining, rateLimit.Limit, rateLimit.Reset.Time().String(),
				)

				// We start this in a separate goroutine, so we can process the results concurrently
				go func() {
					left := len(unscrapedDevelopers)
					low := 0
					high := rateLimit.Remaining
					batchNo := 0
					for left > 0 {
						// Queue up all the jobs from ranges low -> high
						finishedJobs = make(chan int, high-low)
						log.INFO.Printf(
							"batchNo: %d) There are %d developers left to queue up for updating. Queueing developers %d -> %d",
							batchNo, left, low, high,
						)
						queueDeveloperRange(low, high)
						left -= high - low

						// We don't need to sleep if there are no jobs left
						if left > 0 {
							// Figure out the sleepDuration until the reset time and sleep until then
							sleepDuration := rateLimit.Reset.Time().Sub(time.Now().UTC())
							log.INFO.Printf(
								"batchNo: %d) We are going to sleep until %s (%s)",
								batchNo, rateLimit.Reset.Time().String(), sleepDuration.String(),
							)
							sleepBar(sleepDuration)
						} else {
							log.INFO.Printf(
								"Left is 0, so we are not going to sleep until %s because we have finished",
								rateLimit.Reset.Time().String(),
							)
						}

						// We wait until all the results have been processed by the consumer
						developerNumbersSeen := mapset.NewSet[int]()
						for developerNumbersSeen.Cardinality() != high-low {
							log.INFO.Printf("Developers seen %d/%d", developerNumbersSeen.Cardinality(), high-low)
							select {
							case developerNo := <-finishedJobs:
								log.INFO.Printf("Consumer has been notified that job %d has finished. Adding to set...", developerNo)
								developerNumbersSeen.Add(developerNo)
							case <-time.After(2 * time.Second):
								log.INFO.Printf("Did not get any finished jobs in 2s, trying again...")
							}
						}
						close(finishedJobs)

						// Check if there is a new rate limit
						myTwitter.Client.Mutex.Lock()
						rateLimit, ok = myTwitter.Client.RateLimits[myTwitter.RecentSearch]
						myTwitter.Client.Mutex.Unlock()
						log.INFO.Printf("batchNo: %d) I'm awake. New RateLimit?: %t", batchNo, ok)

						// Set low and high for the next batch
						low = high
						if ok && rateLimit.Reset.Time().After(time.Now().UTC()) {
							high += rateLimit.Remaining
						} else {
							high += myTwitter.RecentSearch.Binding().RequestRateLimit.Requests
						}

						// Clamp high to the length of unscraped developers
						if high > len(unscrapedDevelopers) {
							high = len(unscrapedDevelopers)
						}
						log.INFO.Printf("batchNo: %d) Setting low = %d, high = %d", batchNo, low, high)
						batchNo++
					}
					producerDone <- struct{}{}
				}()
			} else {
				err = myTwitter.TemporaryWrap(
					false,
					err,
					"do not know how long there is left on the rate limit for RecentSearch as it is stale",
				)
				return
			}
		} else {
			// Otherwise, queue up the jobs for ALL the updateDeveloperWorkers
			go func() {
				log.INFO.Printf("Queueing up jobs one after another...")
				queueDeveloperRange(0, len(unscrapedDevelopers))
				producerDone <- struct{}{}
			}()
		}

		// We start the consumer of the Update developer results in its own goroutine so we can
		go func() {
			for result := range results {
				if result.error != nil && !myTwitter.IsTemporary(result.error) {
					consumerDone <- errors.Wrapf(result.error, "permanent error occurred whilst scraping unscraped developer %s", result.developer.Username)
					return
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
						log.INFO.Printf(
							"This is the first time we have merged developer %s (%s) back into developerSnapshots."+
								" There are %d snapshots for it (%d after copying)",
							result.developer.Username, result.developer.ID, len(developerSnaps), len(developerSnapshots[id]),
						)
					} else {
						developerSnapshots[id] = append(developerSnapshots[id], developerSnaps...)
					}
				}

				finishedJobs <- result.developerNo
			}
			consumerDone <- nil
		}()

		// First we wait for the producer to stop producing jobs. Once it has, we can also close the jobs queue.
		<-producerDone
		log.INFO.Printf("Producer has finished")
		close(jobs)
		log.INFO.Printf("Closed jobs channel")

		// We then wait for all the workers to process their outstanding jobs.
		updateDeveloperWorkerWg.Wait()
		log.INFO.Printf("Workers have all stopped")

		// Then, we first close the results channel as there should be no more results to queue, and then we wait
		// for the consumer to finish.
		close(results)
		log.INFO.Printf("Closed results channel")
		err = <-consumerDone
		log.INFO.Printf("Consumer has finished")

		// Finally, we wait for the game scrapers to finish
		close(gameScrapeQueue)
		log.INFO.Printf("Closed gameScrapeQueue")
		gameScrapeWg.Wait()
		log.INFO.Printf("Game scrape workers have all stopped")

		// If the consumer returned an error then we will wrap it and return it.
		if err != nil {
			err = errors.Wrap(err, "error occurred in update phase consumer")
			return
		}
	}

	log.INFO.Printf("After running the update phase there are:")
	log.INFO.Printf("\tThere are %d userTweetTimes", len(userTweetTimes))
	log.INFO.Printf("\tThere are %d developerSnapshots", len(developerSnapshots))
	log.INFO.Printf("\tThere are %d gameIDs", gameIDs.Cardinality())
	return
}
