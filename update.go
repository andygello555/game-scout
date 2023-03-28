package main

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/api"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/andygello555/game-scout/reddit"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/andygello555/gotils/v2/numbers"
	"github.com/andygello555/gotils/v2/slices"
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
	totalResources int,
	gameScrapers *models.StorefrontScrapers[string],
	state *ScoutState,
) (gameIDs mapset.Set[uuid.UUID], err error) {
	log.INFO.Printf("Updating info for Developer %v", developer)
	var developerSnap *models.DeveloperSnapshot
	if developerSnap, err = developer.LatestDeveloperSnapshot(db.DB); err != nil {
		log.WARNING.Printf("Could not get latest DeveloperSnapshot for Developer %v in UpdateDeveloper", developer)
		return gameIDs, myErrors.TemporaryWrapf(
			true, err, "could not get latest DeveloperSnapshot for %s in UpdateDeveloper",
			developer.ID,
		)
	}
	if developerSnap == nil {
		log.WARNING.Printf("Could not get latest DeveloperSnapshot for Developer %v", developer)
		return gameIDs, myErrors.TemporaryErrorf(
			true,
			"could not get latest DeveloperSnapshot for %s as there are no DeveloperSnapshots for it",
			developer.ID,
		)
	}
	log.INFO.Printf("Developer %v latest DeveloperSnapshot is version %d and was created on %s", developer, developerSnap.Version, developerSnap.CreatedAt.String())

	// First we update the user's games that exist in the DB
	var games []*models.Game
	gameIDs = mapset.NewThreadUnsafeSet[uuid.UUID]()
	if games, err = developer.Games(db.DB); err != nil {
		log.WARNING.Printf("Could not find any Games for Developer %v", developer)
		return gameIDs, myErrors.TemporaryWrapf(true, err, "could not find Games for Developer %v", developer)
	}
	log.INFO.Printf("Developer %v has %d game(s)", developer, len(games))

	// Update all the games. We just start a new goroutine for each game, so we don't block this goroutine and can
	// continue onto fetching the set of tweets.
	for _, game := range games {
		gameIDs.Add(game.ID)
		state.GetCachedField(StateType).SetOrAdd("Result", "UpdateStats", "Games", models.SetOrAddInc.Func())
		go func(game *models.Game) {
			log.INFO.Printf(
				"Queued update for Game \"%s\" (%s) for Developer %v",
				game.Name.String, game.ID.String(), developer,
			)
			if gameChannel, ok := gameScrapers.Add(true, game, nil); ok {
				<-gameChannel
				log.INFO.Printf(
					"Updated Game \"%s\" (%s) for Developer %v",
					game.Name.String, game.ID.String(), developer,
				)
			}
		}(game)
	}

	startTime := developerSnap.LastTweetTime.UTC().Add(time.Second)
	sevenDaysAgo := time.Now().UTC().Add(time.Hour * time.Duration(-24*7))
	// If the startTime is before the last seven days then we will clamp it to seven days ago.
	// Note: This is because RecentSearch can only look through the past 7 days of tweets.
	if startTime.Before(sevenDaysAgo) {
		log.WARNING.Printf("Clamping startTime for Developer %v to %s", developer, sevenDaysAgo.Format(time.RFC3339))
		startTime = sevenDaysAgo
	}

	// Depending on the developer's type we will take different steps to fetch the PostIterable for it.
	var posts PostIterable
	var userTimesType, devSnapsType CachedFieldType
	switch developer.Type {
	case models.TwitterDeveloperType:
		userTimesType, devSnapsType = UserTweetTimesType, DeveloperSnapshotsType
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
			MaxResults: totalResources,
			// We start looking for tweets after the last tweet time of the latest developer snapshot for the developer
			StartTime: startTime.UTC().Add(time.Second),
		}

		// Then we make the request for totalTweets from RecentSearch
		log.INFO.Printf(
			"Making RecentSearch request for %d tweets from %s for Developer %v",
			totalResources, opts.StartTime.Format(time.RFC3339), developer,
		)
		var result myTwitter.BindingResult
		if result, err = myTwitter.Client.ExecuteBinding(myTwitter.RecentSearch, &myTwitter.BindingOptions{Total: totalResources}, query, opts); err != nil {
			log.ERROR.Printf(
				"Could not fetch %d tweets for Developer %v after %s: %s",
				totalResources, developer, opts.StartTime.Format(time.RFC3339), err.Error(),
			)
			return gameIDs, errors.Wrapf(
				err, "could not fetch %d tweets for Developer %v after %s",
				totalResources, developer, opts.StartTime.Format(time.RFC3339),
			)
		}

		// Then we run DiscoveryBatch with this batch of tweets
		tweetRaw := result.Raw().(*twitter.TweetRaw)

		log.INFO.Printf(
			"Executing DiscoveryBatch for batch of %d tweets for Developer %v",
			len(tweetRaw.TweetDictionaries()), developer,
		)
		posts = Tweets(tweetRaw.TweetDictionaries())
	case models.RedditDeveloperType:
		userTimesType, devSnapsType = RedditUserPostTimesType, RedditDeveloperSnapshotsType
		subreddits := mapset.NewSet[string](globalConfig.Reddit.RedditSubreddits()...)

		// Sleep until user_about rate limit reset (if we need to)
		reddit.API.Client.(*reddit.Client).SleepUntilReset("user_about")

		var user any
		start := time.Now().UTC()
		if user, err = reddit.API.Execute("user_about", developer.Username); err != nil {
			log.ERROR.Printf("Error occurred in the \"user_about\" binding for Reddit Developer %v: %v", developer, err)
		}
		log.INFO.Printf(
			"Retrieved \"user_about\" for Reddit Developer %v in %s",
			developer, time.Now().UTC().Sub(start).String(),
		)

		var paginator api.Paginator[any, any]
		if paginator, err = reddit.API.Paginator(
			"user_where", globalConfig.Scrape.Constants.RedditBindingPaginatorWaitTime.Duration,
			developer.Username, reddit.Overview, reddit.New, reddit.Week, 100,
		); err != nil {
			err = myErrors.TemporaryWrap(false, err, "could not create Paginator for \"user_where\" Reddit binding")
			return
		}

		// We will keep paginating until we have totalResources number of posts and comments, or the earliest post in
		// the batch is before the startTime, or we run out of pages.
		// Each post and comment must be written on one of the subreddits in the subreddits list within the config, as
		// well as being created after the startTime calculated above.
		var pages any
		start = time.Now().UTC()
		if pages, err = paginator.Until(func(paginator api.Paginator[any, any], pages any) bool {
			var (
				earliestTime            time.Time
				postCount, commentCount int
			)

			checkEarliest := func(t time.Time) {
				if earliestTime.IsZero() {
					earliestTime = t
				} else if earliestTime.After(t) {
					earliestTime = t
				}
			}

			listing := pages.(*reddit.Listing)
			if listing != nil {
				for _, post := range listing.Children.Posts {
					checkEarliest(post.Created.Time)
					if subreddits.Contains(post.SubredditName) && post.Created.After(startTime) {
						postCount++
					}
				}

				for _, comment := range listing.Children.Comments {
					checkEarliest(comment.Created.Time)
					if subreddits.Contains(comment.SubredditName) && comment.Created.After(startTime) {
						commentCount++
					}
				}
				return earliestTime.After(startTime) && (postCount < totalResources || commentCount < totalResources)
			}
			// If the listing is nil, then we will return true so we can fetch another page. This usually, happens
			// before the first page is fetched.
			return true
		}); err != nil {
			log.ERROR.Printf("Error occurred whilst fetching Posts and Comments for Reddit Developer %v: %v", developer, err)
		}

		// Filter out all posts and comments that are not from the approved subreddits or are before startTime
		listing := pages.(*reddit.Listing)
		listing.Children.Posts = slices.Filter(listing.Children.Posts, func(idx int, post *reddit.Post, arr []*reddit.Post) bool {
			return subreddits.Contains(post.SubredditName) && post.Created.After(startTime)
		})
		listing.Children.Comments = slices.Filter(listing.Children.Comments, func(idx int, comment *reddit.Comment, arr []*reddit.Comment) bool {
			return subreddits.Contains(comment.SubredditName) && comment.Created.After(startTime)
		})

		log.INFO.Printf(
			"Found %d/%d posts and %d/%d comments for Reddit Developer %v after %s from %d subreddits for updating in %s",
			len(listing.Children.Posts), totalResources, len(listing.Children.Comments), totalResources, developer,
			startTime.Format("2006-01-02"), subreddits.Cardinality(), time.Now().UTC().Sub(start).String(),
		)

		// Construct out PostIterable for DiscoveryBatch
		posts = Posts(slices.Comprehension(
			listing.Children.Posts,
			func(idx int, post *reddit.Post, arr []*reddit.Post) *PostCommentsAndUser {
				return &PostCommentsAndUser{
					PostAndComments: &reddit.PostAndComments{
						Post: post,
						Comments: slices.Filter(listing.Children.Comments, func(idx int, comment *reddit.Comment, arr []*reddit.Comment) bool {
							return post.FullID == comment.PostID
						}),
					},
					User: user.(*reddit.User),
				}
			},
		))
	default:
		err = myErrors.TemporaryErrorf(
			false, "cannot run UpdateDeveloper procedure for %s developer %v",
			developer.Type.String(), developer,
		)
		return
	}

	if posts.Len() > 0 {
		// Run DiscoveryBatch for this batch of tweet dictionaries (if there are any)
		var subGameIDs mapset.Set[uuid.UUID]
		if subGameIDs, err = DiscoveryBatch(
			developerNo, posts, gameScrapers, state,
		); err != nil && !myErrors.IsTemporary(err) {
			log.FATAL.Printf("Error returned by DiscoveryBatch is not temporary: %s. We have to stop :(", err.Error())
			return gameIDs, myErrors.TemporaryWrapf(
				false, err, "could not execute DiscoveryBatch for %s fetched for Developer %v",
				posts.UnitName()+"s", developer,
			)
		}
		// MergeIterableCachedFields the gameIDs from DiscoveryBatch back into the local gameIDs
		gameIDs = gameIDs.Union(subGameIDs)
	} else {
		log.WARNING.Printf(
			"Cannot fetch any %s for Developer %v, inserting a dummy snapshot and time",
			posts.UnitName()+"s", developer,
		)

		state.GetCachedField(userTimesType).SetOrAdd(developer.ID, startTime)
		snapshot := &models.DeveloperSnapshot{
			TimesHighlighted: developer.TimesHighlighted,
			DeveloperID:      developer.ID,
			Tweets:           0,
		}

		switch developer.Type {
		case models.TwitterDeveloperType:
			snapshot.TweetIDs = []string{}
			snapshot.TweetsPublicMetrics = &twitter.TweetMetricsObj{}
			snapshot.UserPublicMetrics = &twitter.UserMetricsObj{}
			snapshot.ContextAnnotationSet = myTwitter.NewContextAnnotationSet()
		case models.RedditDeveloperType:
			snapshot.RedditPostIDs = []string{}
			snapshot.PostPublicMetrics = &models.RedditPostMetrics{}
			snapshot.RedditPublicMetrics = &models.RedditUserMetrics{}
		}

		state.GetIterableCachedField(devSnapsType).SetOrAdd(developer.ID, snapshot)
	}
	return
}

// updateDeveloperJob represents a job that is sent to an updateDeveloperWorker.
type updateDeveloperJob struct {
	id            uuid.UUID
	developerNo   int
	developer     *models.Developer
	totalRequests int
}

// updateDeveloperResult represents a result that is produced by an updateDeveloperWorker.
type updateDeveloperResult struct {
	id          uuid.UUID
	developerNo int
	developer   *models.Developer
	tempState   *ScoutState
	error       error
}

// updateDeveloperWorker is a worker for UpdateDeveloper that takes a channel of updateDeveloperJob and queues up the
// result of UpdateDeveloper as an updateDeveloperResult.
func updateDeveloperWorker(
	wg *sync.WaitGroup,
	jobs <-chan *updateDeveloperJob,
	results chan<- *updateDeveloperResult,
	gameScrapers *models.StorefrontScrapers[string],
) {
	defer wg.Done()
	for job := range jobs {
		result := &updateDeveloperResult{
			id:          job.id,
			developerNo: job.developerNo,
			developer:   job.developer,
			tempState:   StateInMemory(),
		}
		result.tempState.GetCachedField(StateType).SetOrAdd("Phase", Update)
		var subGameIDs mapset.Set[uuid.UUID]
		subGameIDs, result.error = UpdateDeveloper(
			job.developerNo, job.developer, job.totalRequests,
			gameScrapers, result.tempState,
		)
		result.tempState.GetIterableCachedField(GameIDsType).Merge(&GameIDs{subGameIDs})
		result.developer = job.developer
		results <- result
	}
}

// queueDeveloperRange will queue the given range from the given slice of models.Developer(s) and return the Set
// of UUIDs used for each job. The total number of resources to fetch for each models.Developer should also be provided.
func queueDeveloperRange(jobs chan<- *updateDeveloperJob, unscrapedDevelopers []*models.Developer, batchSize, totalResources, low, high int) mapset.Set[uuid.UUID] {
	jobIds := mapset.NewSet[uuid.UUID]()
	slicedUnscrapedDevelopers := unscrapedDevelopers[low:high]
	currentBatchQueue := 0
	for d, developer := range slicedUnscrapedDevelopers {
		log.INFO.Printf("Queued update for Developer no. %d: %v", d, developer)
		job := &updateDeveloperJob{
			id:            uuid.New(),
			developerNo:   d,
			developer:     developer,
			totalRequests: totalResources,
		}
		jobIds.Add(job.id)
		jobs <- job
		currentBatchQueue++
		// When the current number jobs sent to the updateDeveloperWorkers has reached batchSize, we will wait
		// for a bit before continuing to send them
		if currentBatchQueue == batchSize {
			currentBatchQueue = 0
			log.INFO.Printf(
				"We have queued up %d jobs to the updateDeveloperWorkers so we will take a break of %s",
				batchSize, globalConfig.Scrape.Constants.SecondsBetweenUpdateBatches.String(),
			)
			sleepBar(globalConfig.Scrape.Constants.SecondsBetweenUpdateBatches.Duration)
		}
	}
	return jobIds
}

func twitterBatchProducer(
	jobs chan<- *updateDeveloperJob,
	finishedJobs chan uuid.UUID,
	producerDone chan<- struct{},
	unscrapedDevelopers []*models.Developer,
	rateLimit *twitter.RateLimit,
	batchSize, totalTweetsForEachDeveloper int,
) {
	var ok bool
	left := len(unscrapedDevelopers)
	low := 0
	high := rateLimit.Remaining
	batchNo := 0
	for left > 0 {
		// Queue up all the jobs from ranges low -> high
		log.INFO.Printf(
			"Twitter batchNo: %d) There are %d Twitter developers left to queue up for updating. Queueing Twitter developers %d -> %d (%d total)",
			batchNo, left, low, high, high-low,
		)
		jobIds := queueDeveloperRange(jobs, unscrapedDevelopers, batchSize, totalTweetsForEachDeveloper, low, high)
		totalJobs := jobIds.Cardinality()
		if totalJobs != high-low {
			log.ERROR.Printf("queueDeveloperRange could only queue up %d Twitter jobs instead of %d", totalJobs, high-low)
		}
		left -= high - low

		// We don't need to sleep if there are no jobs left
		if left > 0 {
			// Figure out the sleepDuration until the reset time and sleep until then
			sleepDuration := rateLimit.Reset.Time().Sub(time.Now().UTC())
			log.INFO.Printf(
				"Twitter batchNo: %d) We are going to sleep until %s (%s)",
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
		for jobIds.Cardinality() != 0 {
			log.INFO.Printf("Twitter Developers seen %d/%d", totalJobs-jobIds.Cardinality(), totalJobs)
			select {
			case id := <-finishedJobs:
				log.INFO.Printf("Twitter consumer has been notified that job %s has finished. Ticking off...", id.String())
				jobIds.Remove(id)
			case <-time.After(2 * time.Second):
				log.INFO.Printf("Twitter producer did not get any finished jobs in 2s, trying again...")
			}
		}

		// Check if there is a new rate limit
		myTwitter.Client.Mutex.Lock()
		rateLimit, ok = myTwitter.Client.RateLimits[myTwitter.RecentSearch]
		myTwitter.Client.Mutex.Unlock()
		log.INFO.Printf("Twitter batchNo: %d) I'm awake. New RateLimit?: %t", batchNo, ok)

		// Set low and high for the next batch
		low = high
		if ok && rateLimit.Reset.Time().After(time.Now().UTC()) {
			high += rateLimit.Remaining
		} else {
			high += myTwitter.RecentSearch.Binding().RequestRateLimit.Requests
		}

		// Clamp high to the length of unscraped developers
		high = numbers.Clamp(high, len(unscrapedDevelopers))
		log.INFO.Printf("Twitter batchNo: %d) Setting low = %d, high = %d", batchNo, low, high)
		batchNo++
	}
	producerDone <- struct{}{}
}

func redditBatchProducer(
	jobs chan<- *updateDeveloperJob,
	finishedJobs chan uuid.UUID,
	producerDone chan<- struct{},
	unscrapedDevelopers []*models.Developer,
	batchSize, totalPostsForEachDeveloper int,
) {
	left := len(unscrapedDevelopers)
	low := 0
	high := batchSize
	batchNo := 0
	for left > 0 {
		// Queue up all the jobs from ranges low -> high
		log.INFO.Printf(
			"Reddit batchNo: %d) There are %d Reddit developers left to queue up for updating. Queueing Reddit developers %d -> %d (%d total)",
			batchNo, left, low, high, high-low,
		)
		jobIds := queueDeveloperRange(jobs, unscrapedDevelopers, batchSize, totalPostsForEachDeveloper, low, high)
		totalJobs := jobIds.Cardinality()
		if totalJobs != high-low {
			log.ERROR.Printf("queueDeveloperRange could only queue up %d Reddit jobs instead of %d", totalJobs, high-low)
		}
		left -= high - low

		// We don't need to sleep if there are no jobs left
		if left > 0 {
			sleepDuration := time.Minute * 5
			log.INFO.Printf(
				"Reddit batchNo: %d) We are going to sleep for %s",
				batchNo, sleepDuration.String(),
			)
			sleepBar(sleepDuration)
		}

		// We wait until all the results have been processed by the consumer
		for jobIds.Cardinality() != 0 {
			log.INFO.Printf("Reddit Developers seen %d/%d", totalJobs-jobIds.Cardinality(), totalJobs)
			select {
			case id := <-finishedJobs:
				log.INFO.Printf("Reddit consumer has been notified that job %s has finished. Ticking off...", id.String())
				jobIds.Remove(id)
			case <-time.After(2 * time.Second):
				log.INFO.Printf("Reddit producer did not get any finished jobs in 2s, trying again...")
			}
		}
		log.INFO.Printf("Reddit batchNo: %d) I'm awake", batchNo)

		// Set low and high for the next batch
		low = high
		high += batchSize

		// Clamp high to the length of unscraped developers
		high = numbers.Clamp(high, len(unscrapedDevelopers))
		log.INFO.Printf("Reddit batchNo: %d) Setting low = %d, high = %d", batchNo, low, high)
		batchNo++
	}
	producerDone <- struct{}{}
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
func UpdatePhase(developerIDs []string, state *ScoutState) (err error) {
	// Set the batchSize and discoveryTweets vars by reading from ScoutState
	stateState := state.GetCachedField(StateType).(*State)
	batchSize := stateState.BatchSize
	discoveryTweets := stateState.DiscoveryTweets

	// Find the enabled developers that don't exist in the developerIDs array and don't exist in the UpdatedDevelopers
	// State field.
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
		).Session(&gorm.Session{})
	} else {
		unscrapedDevelopersQuery = db.DB.Select(
			"developers.*",
		).Joins(
			"INNER JOIN developer_snapshots ON developer_snapshots.developer_id = developers.id",
		).Where("NOT developers.disabled").Group(
			"developers.id",
		).Having(
			"COUNT(developer_snapshots.id) > 0",
		).Session(&gorm.Session{})
	}

	if unscrapedDevelopersQuery.Error != nil {
		err = myErrors.TemporaryWrap(false, unscrapedDevelopersQuery.Error, "cannot construct query for unscraped developers")
		return
	}

	var unscrapedTwitterDevelopers, unscrapedRedditDevelopers []*models.Developer
	if err = unscrapedDevelopersQuery.Where("developers.type = ?", models.TwitterDeveloperType).Model(&models.Developer{}).Find(&unscrapedTwitterDevelopers).Error; err != nil {
		log.WARNING.Printf("Cannot fetch Twitter developers that were not scraped in the discovery phase: %s", err.Error())
	} else {
		log.INFO.Printf("There are %d Twitter developer's that were not scraped in the discovery phase that exist in the DB", len(unscrapedTwitterDevelopers))
	}

	if err = unscrapedDevelopersQuery.Where("developers.type = ?", models.RedditDeveloperType).Model(&models.Developer{}).Find(&unscrapedRedditDevelopers).Error; err != nil {
		log.WARNING.Printf("Cannot fetch Reddit developers that were not scraped in the discovery phase: %s", err.Error())
	} else {
		log.INFO.Printf("There are %d Reddit developer's that were not scraped in the discovery phase that exist in the DB", len(unscrapedRedditDevelopers))
	}

	totalUnscrapedDevelopers := len(unscrapedTwitterDevelopers) + len(unscrapedRedditDevelopers)
	log.INFO.Printf("Twitter developers comprise %.2ff%% of total unscraped developers (%d)", float64(len(unscrapedTwitterDevelopers))/float64(totalUnscrapedDevelopers)*100.0, totalUnscrapedDevelopers)
	log.INFO.Printf("Reddit developers comprise %.2f%% of total unscraped developers (%d)", float64(len(unscrapedRedditDevelopers))/float64(totalUnscrapedDevelopers)*100.0, totalUnscrapedDevelopers)
	if totalUnscrapedDevelopers == 0 {
		log.WARNING.Printf("There are no unscraped developers to update, so we will finished here")
		return
	}

	// Set up channels, wait group, and sync.Once for pipeline.
	jobs := make(chan *updateDeveloperJob, totalUnscrapedDevelopers)
	results := make(chan *updateDeveloperResult, totalUnscrapedDevelopers)
	// Synchronizing worker goroutines
	var updateDeveloperWorkerWg sync.WaitGroup
	// Indicates when the Twitter producer has finished
	twitterProducerDone := make(chan struct{})
	// Indicates when the Reddit producer has finished
	redditProducerDone := make(chan struct{})
	// Indicated when the consumer has finished (as well as with what error)
	consumerDone := make(chan error)
	// Indicates which Twitter jobs have finished in the current producer batch
	finishedTwitterJobs := make(chan uuid.UUID, len(unscrapedTwitterDevelopers))
	// Indicates which Reddit jobs have finished in the current producer batch
	finishedRedditJobs := make(chan uuid.UUID, len(unscrapedRedditDevelopers))

	// For Twitter Developers...
	// For each unscraped developer we will execute a query on RecentSearch for the developer for tweets after the
	// latest developer snapshot's LastTweetTime. We decide how many tweets to request for each developer by dividing
	// the remaining number of tweets for this day by the number of developers we need to update.
	totalTweetsForEachDeveloper := (int(globalConfig.Twitter.RateLimits.TweetsPerDay) - discoveryTweets) / len(unscrapedTwitterDevelopers)
	totalTweetsForEachDeveloper = numbers.Clamp(totalTweetsForEachDeveloper, globalConfig.Scrape.Constants.MaxUpdateTweets)
	log.INFO.Printf("Initial number of tweets fetched for %d developers is %d", len(unscrapedTwitterDevelopers), totalTweetsForEachDeveloper)

	// In the case that we can't get enough tweets to satisfy the minimum resources per-request for RecentSearch for
	// any developers we will exclude developers one by one based on their latest developer snapshot
	if totalTweetsForEachDeveloper < myTwitter.RecentSearch.Binding().MinResourcesPerRequest {
		log.WARNING.Printf(
			"totalTweetsForEachDeveloper is %d. Reducing the number of Twitter developers to see if we can fetch "+
				"at least %d tweet(s) for each Twitter developer",
			totalTweetsForEachDeveloper, myTwitter.RecentSearch.Binding().MinResourcesPerRequest,
		)
		initialDeveloperCount := len(unscrapedRedditDevelopers)
		checkDeveloperNo := initialDeveloperCount - 1
		// We iterate until totalTweetsForEachDeveloper is at least the minimum resources per-request for the
		// RecentSearch binding, or checkDeveloper is zero (we cannot make this request as there are too many
		// developers)
		for totalTweetsForEachDeveloper < myTwitter.RecentSearch.Binding().MinResourcesPerRequest && checkDeveloperNo > 0 {
			unscrapedDevelopersQuery.Model(&models.Developer{}).Joins(
				"left join developer_snapshots on developer_snapshots.developer_id = developer.id",
			).Where(
				"developer_snapshots.weighted_score IS NOT NULL AND developers.type = ?",
				models.TwitterDeveloperType,
			).Order(
				"version desc, weighted_score desc",
			).Limit(checkDeveloperNo).Find(&unscrapedTwitterDevelopers)
			// We re-calculate totalTweetsForEachDeveloper on each iteration
			totalTweetsForEachDeveloper = (int(globalConfig.Twitter.RateLimits.TweetsPerDay) - discoveryTweets) / len(unscrapedTwitterDevelopers)
			checkDeveloperNo--
		}
		// If we end up with zero developers we will return a non-temporary error
		if checkDeveloperNo == 0 {
			log.FATAL.Printf(
				"Could not get totalTweetsForEachDeveloper up to at least %d, because there is just not "+
					"enough tweetcap left for today (%d rem.) to update %d Twitter developers",
				myTwitter.RecentSearch.Binding().MinResourcesPerRequest,
				int(globalConfig.Twitter.RateLimits.TweetsPerDay)-discoveryTweets,
				initialDeveloperCount,
			)
			err = myErrors.TemporaryErrorf(
				false, "cannot run update phase as we couldn't find a totalTweetsForEachDeveloper "+
					"number that didn't result in 0 unscrapedTwitterDevelopers",
			)
			return
		}
		log.INFO.Printf(
			"Found a new totalTweetsForEachDeveloper: %d, by reducing Twitter developer count to %d",
			totalTweetsForEachDeveloper, len(unscrapedTwitterDevelopers),
		)
	}

	// For Reddit Developers...
	// Because Reddit scraping is not limited to any particular maximum number of posts, so we need to find the
	// number of requests made today using the default Reddit client's request counter.
	redditRequestsMadeToday := int(reddit.API.Client.(*reddit.Client).RequestsMadeWithinPeriod(time.Hour * 24))
	totalPostsForEachDeveloper := (int(globalConfig.Reddit.RateLimits.RequestsPerDay) - redditRequestsMadeToday) / len(unscrapedRedditDevelopers)
	totalPostsForEachDeveloper = numbers.Clamp(totalPostsForEachDeveloper, globalConfig.Scrape.Constants.MaxUpdateTweets)
	log.INFO.Printf("Number of Reddit posts to fetch for %d developers is %d", len(unscrapedRedditDevelopers), totalTweetsForEachDeveloper)

	// Start the workers for scraping games.
	gameScrapers := models.NewStorefrontScrapers[string](
		globalConfig.Scrape, db.DB,
		globalConfig.Scrape.Constants.UpdateGameScrapeWorkers,
		globalConfig.Scrape.Constants.UpdateMaxConcurrentGameScrapeWorkers,
		(len(unscrapedTwitterDevelopers)*totalTweetsForEachDeveloper*globalConfig.Scrape.Constants.MaxGamesPerTweet)+(len(unscrapedRedditDevelopers)*totalPostsForEachDeveloper*globalConfig.Scrape.Constants.MaxGamesPerPost),
		globalConfig.Scrape.Constants.MinScrapeStorefrontsForGameWorkerWaitTime.Duration,
		globalConfig.Scrape.Constants.MaxScrapeStorefrontsForGameWorkerWaitTime.Duration,
	)
	gameScrapers.Start()

	// Start the workers
	for w := 0; w < globalConfig.Scrape.Constants.UpdateDeveloperWorkers; w++ {
		log.INFO.Printf("Starting updateDeveloperWorker no. %d", w)
		updateDeveloperWorkerWg.Add(1)
		go updateDeveloperWorker(&updateDeveloperWorkerWg, jobs, results, gameScrapers)
	}

	// We also check if the Twitter rate limit will be exceeded by the number of requests. We use 100 *
	// unscrapedDevelopers as the totalResources arg as we are more interested if the number of requests can be
	// made.
	if err = myTwitter.Client.CheckRateLimit(myTwitter.RecentSearch.Binding(), 100*len(unscrapedTwitterDevelopers)); err != nil {
		log.WARNING.Printf(
			"%d requests for %d Twitter developers will exceed our rate limit. We need to check if we can batch...",
			len(unscrapedTwitterDevelopers), len(unscrapedTwitterDevelopers),
		)

		myTwitter.Client.Mutex.Lock()
		rateLimit, ok := myTwitter.Client.RateLimits[myTwitter.RecentSearch]
		myTwitter.Client.Mutex.Unlock()

		// If we cannot find the rate limit, or it has already ended then we will keep making singleton requests
		// until we refresh the rate limit.
		for !ok || rateLimit.Reset.Time().Before(time.Now().UTC()) {
			log.ERROR.Printf(
				"Rate limit for RecentSearch doesn't exist or is stale (ok = %t, rateLimit = %v)",
				ok, rateLimit,
			)

			// Make a single throwaway query
			query := globalConfig.Twitter.TwitterQuery()
			if _, err = myTwitter.Client.ExecuteBinding(
				myTwitter.RecentSearch,
				&myTwitter.BindingOptions{Total: 10},
				query,
				twitter.TweetRecentSearchOpts{MaxResults: 10},
			); err != nil {
				log.ERROR.Printf(
					"Could not make RecentSearch to refresh rate limit: %s, sleeping for 20s",
					err.Error(),
				)
				sleepBar(time.Second * 20)
			}

			// Get the rate limit again
			myTwitter.Client.Mutex.Lock()
			rateLimit, ok = myTwitter.Client.RateLimits[myTwitter.RecentSearch]
			myTwitter.Client.Mutex.Unlock()
		}

		log.WARNING.Printf("Managed to get rate limit for RecentSearch: %v", rateLimit)
		log.WARNING.Printf(
			"Due to the number of unscrapedTwitterDevelopers (%d) this would exceed the current rate limit of "+
				"RecentSearch: %d/%d %s. Switching to a batching approach...",
			len(unscrapedTwitterDevelopers), rateLimit.Remaining, rateLimit.Limit, rateLimit.Reset.Time().String(),
		)

		go twitterBatchProducer(
			jobs, finishedTwitterJobs, twitterProducerDone, unscrapedTwitterDevelopers, rateLimit, batchSize,
			totalTweetsForEachDeveloper,
		)
	} else {
		// Otherwise, queue up the jobs for ALL the updateDeveloperWorkers
		go func() {
			log.INFO.Printf("Queueing up jobs one after another...")
			queueDeveloperRange(
				jobs, unscrapedTwitterDevelopers, batchSize, totalTweetsForEachDeveloper, 0,
				len(unscrapedTwitterDevelopers),
			)
			twitterProducerDone <- struct{}{}
		}()
	}

	// Start the Reddit batched producer. Since the Reddit API Client handles RateLimits in a (kinda) nice way we don't
	// need to worry so much about rate limits so much. Also we don't have a daily maximum number of resources that we
	// can't exceed
	go redditBatchProducer(
		jobs, finishedRedditJobs, redditProducerDone, unscrapedRedditDevelopers,
		int(float64(batchSize)*globalConfig.Scrape.Constants.RedditProducerBatchMultiplier), totalPostsForEachDeveloper,
	)

	// We start the consumer of the Update developer results in its own goroutine, so we can process each result in
	// parallel
	go func(state *ScoutState) {
		// We define a function to queue up finished results into their appropriate
		finished := func(result *updateDeveloperResult) {
			switch result.developer.Type {
			case models.TwitterDeveloperType:
				finishedTwitterJobs <- result.id
			case models.RedditDeveloperType:
				finishedRedditJobs <- result.id
			default:
				log.ERROR.Printf("Consumed a result for Unknown developer %v", result.developer)
			}
		}

		resultBatchCount := 0
		var consumerErr error
		for result := range results {
			if result.error != nil && !myErrors.IsTemporary(result.error) {
				finished(result)
				log.ERROR.Printf(
					"Permanent error in result for Developer %v contains an error: %s",
					result.developer, result.error.Error(),
				)
				consumerErr = myErrors.MergeErrors(consumerErr, errors.Wrapf(
					result.error,
					"permanent error occurred whilst scraping unscraped developer %s",
					result.developer.Username,
				))
				return
			} else if result.error != nil {
				finished(result)
				log.ERROR.Printf("UpdateDeveloper result for %v contains an error: %s", result.developer, result.error.Error())
				continue
			} else {
				log.INFO.Printf("Got UpdateDeveloper result for Developer %v", result.developer)
			}

			// MergeIterableCachedFields the gameIDs, userTweetTimes, and developerSnapshots from the result into
			// the main state.
			log.INFO.Printf(
				"Merging gameIDs, userTweetTimes, and developerSnapshots from the result for Developer %v, back into "+
					"the main ScoutState",
				result.developer,
			)
			state.MergeIterableCachedFields(result.tempState)

			// Add the developer's ID to the UpdatedDevelopers array in the state info, so that if anything bad
			// happens we can always continue the UpdatePhase and ignore the developers that we have already
			// updated.
			state.GetCachedField(StateType).SetOrAdd("UpdatedDevelopers", result.developer.ID)
			state.GetCachedField(StateType).SetOrAdd("Result", "UpdateStats", "Developers", models.SetOrAddInc.Func())

			// Then we add all the properties of the ScoutResult within the temp state in the result returned from
			// UpdateDeveloper.
			scoutResultAny, _ := result.tempState.GetCachedField(StateType).Get("Result")
			scoutResult := scoutResultAny.(*models.ScoutResult)
			log.INFO.Printf("Merging temp ScoutResult properties collected for Developer %v back into main ScoutResult: %#+v", result.developer, scoutResult.UpdateStats)
			state.GetCachedField(StateType).SetOrAdd("Result", "UpdateStats", "Games", models.SetOrAddAdd.Func(scoutResult.UpdateStats.Games))
			state.GetCachedField(StateType).SetOrAdd("Result", "UpdateStats", "TweetsConsumed", models.SetOrAddAdd.Func(scoutResult.UpdateStats.TweetsConsumed))
			state.GetCachedField(StateType).SetOrAdd("Result", "UpdateStats", "PostsConsumed", models.SetOrAddAdd.Func(scoutResult.UpdateStats.PostsConsumed))
			state.GetCachedField(StateType).SetOrAdd("Result", "UpdateStats", "TotalSnapshots", models.SetOrAddAdd.Func(scoutResult.UpdateStats.TotalSnapshots))

			// Save the ScoutState after batchSize number of successful results have been consumed
			resultBatchCount++
			if resultBatchCount == batchSize {
				log.INFO.Printf("We have consumed %d successful results, saving ScoutState...", resultBatchCount)
				if err = state.Save(); err != nil {
					log.ERROR.Printf("Could not save ScoutState: %v", err)
				}
				resultBatchCount = 0
			}

			finished(result)
		}

		// Push the error into the done channel if there is one
		if consumerErr != nil {
			consumerDone <- consumerErr
		} else {
			consumerDone <- nil
		}
	}(state)

	// First we wait for the producers to stop producing jobs. Once they have, we can also close the jobs queue.
	<-twitterProducerDone
	log.INFO.Printf("Twitter producer has finished at %s", time.Now().UTC().String())
	<-redditProducerDone
	log.INFO.Printf("Reddit producer has finished at %s", time.Now().UTC().String())
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
	gameScrapers.Wait()
	log.INFO.Println("StorefrontScrapers have finished")

	// If the consumer returned an error then we will wrap it and return it.
	if err != nil {
		err = errors.Wrap(err, "error occurred in update phase consumer")
		return
	}

	log.INFO.Printf("After running the update phase there are:")
	log.INFO.Printf("\tThere are %d userTweetTimes", state.GetIterableCachedField(UserTweetTimesType).Len())
	log.INFO.Printf("\tThere are %d redditUserPostTimes", state.GetIterableCachedField(RedditUserPostTimesType).Len())
	log.INFO.Printf("\tThere are %d developerSnapshots", state.GetIterableCachedField(DeveloperSnapshotsType).Len())
	log.INFO.Printf("\tThere are %d redditDeveloperSnapshots", state.GetIterableCachedField(RedditDeveloperSnapshotsType).Len())
	log.INFO.Printf("\tThere are %d gameIDs", state.GetIterableCachedField(GameIDsType).Len())
	return
}
