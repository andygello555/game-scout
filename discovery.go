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
	"github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"time"
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
				log.INFO.Printf(
					"Tweet %s from %q is a retweet, remapping to author of retweet %q",
					tweet.Tweet.ID, tweet.Author.UserName, referencedTweet.TweetDictionary.Author.UserName,
				)
				tweet = referencedTweet.TweetDictionary
				break
			}
		}
	}

	if tweetCreatedAt, err = time.Parse(globalConfig.Twitter.CreatedAtFormat, tweet.Tweet.CreatedAt); err != nil {
		err = errors.Wrap(err, "could not parse tweet's created_at")
		return
	}

	developer.ID = tweet.Author.ID
	developerSnap.TweetIDs = []string{tweet.Tweet.ID}
	developerSnap.DeveloperID = developer.ID
	developer.Name = tweet.Author.Name
	developer.Username = tweet.Author.UserName
	developer.PublicMetrics = tweet.Author.PublicMetrics
	developer.Description = tweet.Author.Description
	if developer.ProfileCreated, err = time.Parse(globalConfig.Twitter.CreatedAtFormat, tweet.Author.CreatedAt); err != nil {
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
		for _, storefront := range []models.Storefront{
			models.SteamStorefront,
			models.ItchIOStorefront,
		} {
			// If the expanded URL is in the same format as the ScrapeURL
			if storefront.ScrapeURL().Match(url.ExpandedURL) {
				if _, ok := storefrontMap[storefront]; !ok {
					storefrontMap[storefront] = mapset.NewThreadUnsafeSet[string]()
				}
				if !storefrontMap[storefront].Contains(url.ExpandedURL) {
					storefrontMap[storefront].Add(url.ExpandedURL)
					// Because we know that this is a new game, we will increment the game total
					totalGames++
				}
			}

			// If we have reached the maximum number of games, then we will exit out of these loops
			if totalGames == globalConfig.Scrape.Constants.MaxGamesPerTweet {
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
		if gameChannel, ok := gameScrapers.Add(false, &models.Game{Developers: []string{developer.Username}}, storefrontMap); ok {
			gameModel := <-gameChannel
			if gameModel != nil {
				game = gameModel.(*models.Game)
				// Don't save games that are not games
				if !game.IsGame {
					game = nil
				}
			}
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
	log.INFO.Printf("Starting DiscoveryBatch no. %d", batchNo)

	// Create the job and result channels for the transformTweetWorkers
	jobs := make(chan *transformTweetJob, len(dictionary))
	results := make(chan *transformTweetResult, len(dictionary))
	gameIDs = mapset.NewThreadUnsafeSet[uuid.UUID]()

	// Start the transformTweetWorkers
	for i := 0; i < globalConfig.Scrape.Constants.TransformTweetWorkers; i++ {
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

	phase := func() Phase {
		phaseAny, _ := state.GetCachedField(StateType).Get("Phase")
		return phaseAny.(Phase)
	}

	resultKey := func() string {
		return fmt.Sprintf("%sStats", phase().String())
	}

	resultAddDevOrGame := func(created bool) bool {
		switch phase() {
		case Discovery:
			return created
		case Update:
			return !created
		default:
			return false
		}
	}

	// Dequeue the results and save them
	for i := 0; i < len(dictionary); i++ {
		result := <-results
		if result.err == nil {
			var (
				created bool
				gameErr error
			)

			log.INFO.Printf("%stransformed tweet no. %d successfully", logPrefix, result.tweetNo)
			state.GetIterableCachedField(UserTweetTimesType).SetOrAdd(result.developer.ID, result.tweetCreatedAt)
			state.GetCachedField(StateType).SetOrAdd("Result", resultKey(), "TweetsConsumed", models.SetOrAddInc.Func())

			// At this point we only save the developer and the game. We still need to aggregate all the possible
			// developerSnapshots
			if snapshots, ok := state.GetIterableCachedField(DeveloperSnapshotsType).Get(result.developer.ID); !ok {
				// Create/update the developer if a developer snapshot for it hasn't been added yet
				log.INFO.Printf("%sthis is the first time we have seen Developer %v", logPrefix, result.developer)
				if created, err = db.Upsert(result.developer); resultAddDevOrGame(created) {
					state.GetCachedField(StateType).SetOrAdd("Result", resultKey(), "Developers", models.SetOrAddInc.Func())
				}
			} else {
				log.INFO.Printf(
					"%sthis is NOT the first time we have seen Developer %v, they have %d snapshots",
					logPrefix, result.developer, len(snapshots.([]*models.DeveloperSnapshot)),
				)
			}

			// Create the game
			if created, gameErr = db.Upsert(result.game); created {
				gameIDs.Add(result.game.ID)
			}

			if resultAddDevOrGame(created) && phase() == Discovery {
				state.GetCachedField(StateType).SetOrAdd("Result", resultKey(), "Games", models.SetOrAddInc.Func())
			}

			err = myErrors.MergeErrors(err, gameErr)
			if err != nil {
				return gameIDs, myErrors.TemporaryWrap(false, err, "could not insert either Developer or Game into DB")
			}

			// Add the developerSnap to the developerSnapshots
			state.GetIterableCachedField(DeveloperSnapshotsType).SetOrAdd(result.developer.ID, result.developerSnap)
			state.GetCachedField(StateType).SetOrAdd("Result", resultKey(), "TotalSnapshots", models.SetOrAddInc.Func())
			log.INFO.Printf("%sadded DeveloperSnapshot to mapping (%d developer IDs in DeveloperSnapshots)", logPrefix, state.GetIterableCachedField(DeveloperSnapshotsType).Len())
		} else {
			log.WARNING.Printf("%scouldn't transform tweet no. %d: %s. Skipping...", logPrefix, result.tweetNo, err.Error())
		}
	}
	return
}

type PostCommentsAndUser struct {
	*reddit.PostAndComments
	*reddit.User
}

// SubredditFetch will retrieve the top posts, with their comments, and OP's.
func SubredditFetch(subreddit string, state *ScoutState) (postsCommentsAndUsers []*PostCommentsAndUser, err error) {
	var paginator api.Paginator[any, any]
	if paginator, err = reddit.API.Paginator(
		"top", globalConfig.Scrape.Constants.RedditBindingPaginatorWaitTime.Duration,
		subreddit, reddit.Week, 100,
	); err != nil {
		err = myErrors.TemporaryWrap(false, err, "could not create Paginator for \"top\" Reddit binding")
		return
	}

	start := time.Now()
	log.INFO.Printf("Retrieving all top posts in the last week for %q", subreddit)
	var listing any
	if listing, err = paginator.All(); err != nil {
		log.ERROR.Printf("Could not find all top posts for %q: %v", subreddit, err)
	}

	posts := listing.(*reddit.Listing).Children.Posts
	log.INFO.Printf(
		"Found %d top posts for the week for %q in %s",
		len(posts), subreddit, time.Now().Sub(start).String(),
	)

	skipPosts := 0
	if len(posts) > globalConfig.Scrape.Constants.RedditPostsPerSubreddit {
		skipPosts = len(posts) / globalConfig.Scrape.Constants.RedditPostsPerSubreddit
	}

	postsCommentsAndUsers = make([]*PostCommentsAndUser, 0, len(posts))
	for _, post := range posts {
		start = time.Now()
		log.INFO.Printf(
			"Fetching comments and user for post %q (%s) on subreddit %q",
			post.Title, post.ID, subreddit,
		)

		if paginator, err = reddit.API.Paginator(
			"comments", globalConfig.Scrape.Constants.RedditBindingPaginatorWaitTime.Duration,
			post.ID, &subreddit,
		); err != nil {
			err = myErrors.TemporaryWrapf(
				false, err, "could not create Paginator for \"comments\" for post %q (%s) in %q",
				post.Title, post.ID, subreddit,
			)
			return
		}

		var postAndComments any
		if postAndComments, err = paginator.Until(func(paginator api.Paginator[any, any]) bool {
			return paginator.Page() == nil || paginator.Page().(*reddit.PostAndComments).Count() < globalConfig.Scrape.Constants.RedditCommentsPerPost
		}); err != nil {
			log.ERROR.Printf(
				"Could not find %d comments for post %q (%s), only found %d: %v",
				globalConfig.Scrape.Constants.RedditCommentsPerPost, post.Title, post.ID, postAndComments.(*reddit.PostAndComments).Count(), err,
			)
		}

		postCommentsAndUser := PostCommentsAndUser{PostAndComments: postAndComments.(*reddit.PostAndComments)}
		if postCommentsAndUser.PostAndComments == nil {
			log.WARNING.Printf("PostAndComments is nil, which means we couldn't get past the first page")
			continue
		}

		log.INFO.Printf("Found %d comments for post %q (%s) in %s", postCommentsAndUser.Count(), post.Title, post.ID, time.Now().Sub(start))

		var user any
		if user, err = reddit.API.Execute("user_about", post.Author); err != nil {
			log.WARNING.Printf("Could not get user_about for post %q (%s)'s author %q: %v", post.Title, post.ID, post.Author, err)
			continue
		}
		postCommentsAndUser.User = user.(*reddit.User)
		postsCommentsAndUsers = append(postsCommentsAndUsers, &postCommentsAndUser)
	}
	return
}

type redditSubredditScrapeJob struct {
	subreddit string
	state     *ScoutState
}

type redditSubredditScrapeResult struct {
	redditSubredditScrapeJob
	postsCommentsAndUsers []*PostCommentsAndUser
	err                   error
}

func redditSubredditScraper(jobs <-chan redditSubredditScrapeJob, results chan<- redditSubredditScrapeResult) {
	for job := range jobs {
		result := redditSubredditScrapeResult{redditSubredditScrapeJob: job}
		result.postsCommentsAndUsers, result.err = SubredditFetch(job.subreddit, job.state)
		results <- result
	}
}

// RedditDiscoveryPhase will iterate over each subreddit in the config and find all the top posts for them. It will then
// iterate over each post found, and fetch a max of 100 comments for each post. From these comments, it will filter any
// out that are not the OP's. It will then look in the body of these comments for a link to any game store pages.
//
// This can run in parallel to DiscoveryPhase.
func RedditDiscoveryPhase(state *ScoutState, gameScrapers *models.StorefrontScrapers[string]) (err error) {
	subreddits := globalConfig.Reddit.RedditSubreddits()
	jobs := make(chan redditSubredditScrapeJob, len(subreddits))
	results := make(chan redditSubredditScrapeResult, len(subreddits))

	for i := 0; i < globalConfig.Scrape.Constants.RedditSubredditScrapeWorkers; i++ {
		go redditSubredditScraper(jobs, results)
	}

	for _, subreddit := range subreddits {
		jobs <- redditSubredditScrapeJob{
			subreddit: subreddit,
			state:     StateInMemory(),
		}
	}

	for r := 0; r < len(subreddits); r++ {
		result := <-results
		if result.err != nil {
			log.ERROR.Printf("Subreddit %q could not be scraped: %v", result.subreddit, result.err)
			if !myErrors.IsTemporary(result.err) {
				err = errors.Wrap(result.err, "reddit scrape could not recover from non-temp error")
				return
			}
			continue
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
		globalConfig.Scrape.Constants.DiscoveryGameScrapeWorkers,
		globalConfig.Scrape.Constants.DiscoveryMaxConcurrentGameScrapeWorkers,
		batchSize*globalConfig.Scrape.Constants.MaxGamesPerTweet,
		globalConfig.Scrape.Constants.MinScrapeStorefrontsForGameWorkerWaitTime.Duration,
		globalConfig.Scrape.Constants.MaxScrapeStorefrontsForGameWorkerWaitTime.Duration,
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
			time.Now().UTC().Sub(batchStart).String(), globalConfig.Scrape.Constants.SecondsBetweenDiscoveryBatches.String(),
		)
		sleepBar(globalConfig.Scrape.Constants.SecondsBetweenDiscoveryBatches.Duration)
	}

	gameScrapers.Wait()
	log.INFO.Println("StorefrontScrapers have finished")

	return
}
