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
	"github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"math"
	"time"
)

type PostIterable interface {
	Len() int
	UnitName() string
	Queue(jobs chan<- *TransformJob, args ...any)
}

type Tweets map[string]*twitter.TweetDictionary

func (t Tweets) Len() int         { return len(t) }
func (t Tweets) UnitName() string { return "tweet" }
func (t Tweets) Queue(jobs chan<- *TransformJob, args ...any) {
	tweetNo := 1
	for id, tweet := range t {
		log.INFO.Printf("%squeued tweet no. %d (%s)", args[0].(string), tweetNo, id)
		jobs <- &TransformJob{
			no:    tweetNo,
			tweet: tweet,
		}
		tweetNo++
	}
}

type PostCommentsAndUser struct {
	*reddit.PostAndComments
	*reddit.User
}

type Posts []*PostCommentsAndUser

func (p Posts) Len() int         { return len(p) }
func (p Posts) UnitName() string { return "post" }

func (p Posts) Queue(jobs chan<- *TransformJob, args ...any) {
	for no, post := range p {
		log.INFO.Printf(
			"%squeued post no. %d %q (%s) for subreddit %q",
			args[0].(string), no, post.Post.Title, post.Post.ID, post.Post.SubredditName,
		)
		jobs <- &TransformJob{
			no:   no,
			post: post,
		}
	}
}

// TransformJob wraps the arguments of TransformTweet for TransformWorker.
type TransformJob struct {
	no    int
	tweet *twitter.TweetDictionary
	post  *PostCommentsAndUser
}

// TransformResult wraps the return values of TransformTweet for TransformWorker.
type TransformResult struct {
	no            int
	developer     *models.Developer
	developerSnap *models.DeveloperSnapshot
	game          *models.Game
	postCreatedAt time.Time
	err           error
}

// TransformWorker takes a channel of twitter.TweetDictionary, and queues up the return values of TransformTweet as
// TransformResult in a results channel.
func TransformWorker(
	jobs <-chan *TransformJob,
	results chan<- *TransformResult,
	gameScrapers *models.StorefrontScrapers[string],
) {
	for job := range jobs {
		result := &TransformResult{no: job.no}
		switch {
		case job.tweet == nil:
			result.developer, result.developerSnap, result.game, result.postCreatedAt, result.err = TransformTweet(
				job.tweet,
				gameScrapers,
			)
		case job.post == nil:
			result.developer, result.developerSnap, result.game, result.postCreatedAt, result.err = TransformPost(
				job.post,
				gameScrapers,
			)
		}
		results <- result
	}
}

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
	developer.Type = models.TwitterDeveloperType
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

func TransformPost(post *PostCommentsAndUser, gameScrapers *models.StorefrontScrapers[string]) (
	developer *models.Developer,
	developerSnap *models.DeveloperSnapshot,
	game *models.Game,
	postCreatedAt time.Time,
	err error,
) {
	developer = &models.Developer{}
	developer.ID = post.User.ID
	developer.Name = post.User.Name
	developer.Username = post.User.Name
	developer.Type = models.RedditDeveloperType
	developer.ProfileCreated = post.User.Created.Time
	developer.RedditPublicMetrics = &models.RedditUserMetrics{
		PostKarma:    post.User.PostKarma,
		CommentKarma: post.User.CommentKarma,
	}

	developerSnap = &models.DeveloperSnapshot{}
	developerSnap.DeveloperID = developer.ID
	return
}

// DiscoveryBatch takes a PostIterable and runs TransformTweet/TransformPost on each tweet/post that is within it. It
// also takes fills out the userTweetTimes and developerSnapshots cached fields within the given ScoutState, which are
// passed in from either DiscoveryPhase, RedditDiscoveryPhase, or UpdateDeveloper. Due to the rate limiting by Steam on
// their store API, you can also pass in how many gameWorkers (a worker which scrapes a game) to start, and how many of
// these game workers can be executing at once.
//
// The gameScrapeQueue is the channel to which games are queued to be scraped. This channel, along with the workers,
// should be created and close outside DiscoveryBatch. If DiscoveryBatch is being run in the discovery phase, then it
// will be created in DiscoveryPhase. Otherwise, if it is running in UpdateDeveloper, they should be created in
// UpdatePhase.
func DiscoveryBatch(
	batchNo int,
	posts PostIterable,
	gameScrapers *models.StorefrontScrapers[string],
	state *ScoutState,
) (gameIDs mapset.Set[uuid.UUID], err error) {
	logPrefix := fmt.Sprintf("DiscoveryBatch %d: ", batchNo)
	log.INFO.Printf("Starting DiscoveryBatch no. %d", batchNo)

	// Create the job and result channels for the transformTweetWorkers
	jobs := make(chan *TransformJob, posts.Len())
	results := make(chan *TransformResult, posts.Len())
	gameIDs = mapset.NewThreadUnsafeSet[uuid.UUID]()

	// Start the transformTweetWorkers
	for i := 0; i < globalConfig.Scrape.Constants.TransformTweetWorkers; i++ {
		go TransformWorker(jobs, results, gameScrapers)
	}

	// Queue up all the jobs
	posts.Queue(jobs, logPrefix)
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
	for i := 0; i < posts.Len(); i++ {
		result := <-results
		if result.err == nil {
			var (
				created bool
				gameErr error
			)

			log.INFO.Printf("%stransformed %s no. %d successfully", logPrefix, posts.UnitName(), result.no)
			state.GetIterableCachedField(UserTweetTimesType).SetOrAdd(result.developer.ID, result.postCreatedAt)
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
			log.WARNING.Printf("%scouldn't transform %s no. %d: %s. Skipping...", logPrefix, posts.UnitName(), result.no, err.Error())
		}
	}
	return
}

// SubredditFetch will retrieve the top posts, with their comments, and OP's.
func SubredditFetch(subreddit string) (postsCommentsAndUsers []*PostCommentsAndUser, err error) {
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
	currentSkip := 0
	postsPerSubreddit := globalConfig.Scrape.Constants.RedditPostsPerSubreddit
	postsToScrape := len(posts)
	if len(posts) > postsPerSubreddit {
		skipPosts = int(math.Round(float64(len(posts)) / float64(postsPerSubreddit)))
		currentSkip = skipPosts - 1
		postsToScrape = int(math.Ceil(float64(len(posts)) / float64(skipPosts)))
		log.WARNING.Printf(
			"Because there are %d more total posts on %q (top weekly) than the limit of %d, we will only scrape every %s post (%d posts total)",
			len(posts)-postsPerSubreddit, subreddit, postsPerSubreddit, numbers.Ordinal(skipPosts), postsToScrape,
		)
	}

	postsCommentsAndUsers = make([]*PostCommentsAndUser, 0, len(posts))
	for _, post := range posts {
		if skipPosts > 0 {
			currentSkip++
			if currentSkip == skipPosts {
				currentSkip = 0
			} else {
				log.WARNING.Printf(
					"Skipping post %q (%s) on %q: currentSkip != 0 == %d",
					post.Title, post.ID, subreddit, currentSkip,
				)
				continue
			}
		}

		start = time.Now()
		log.INFO.Printf(
			"%q(%d/%d): Fetching comments and user for post %q (%s) on subreddit %q",
			subreddit, len(postsCommentsAndUsers)+1, postsToScrape, post.Title, post.ID, subreddit,
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
				"%q(%d/%d): Could not find %d comments for post %q (%s), only found %d: %v",
				subreddit, len(postsCommentsAndUsers)+1, postsToScrape,
				globalConfig.Scrape.Constants.RedditCommentsPerPost, post.Title, post.ID,
				postAndComments.(*reddit.PostAndComments).Count(), err,
			)
		}

		postCommentsAndUser := PostCommentsAndUser{PostAndComments: postAndComments.(*reddit.PostAndComments)}
		if postCommentsAndUser.PostAndComments == nil {
			log.WARNING.Printf(
				"%q(%d/%d): PostAndComments is nil, which means we couldn't get past the first page",
				subreddit, len(postsCommentsAndUsers)+1, postsToScrape,
			)
			continue
		}

		log.INFO.Printf(
			"%q(%d/%d): Found %d comments for post %q (%s) in %s",
			subreddit, len(postsCommentsAndUsers)+1, postsToScrape, postCommentsAndUser.Count(), post.Title, post.ID,
			time.Now().Sub(start),
		)

		var user any
		if user, err = reddit.API.Execute("user_about", post.Author); err != nil {
			log.WARNING.Printf(
				"%q(%d/%d): Could not get user_about for post %q (%s)'s author %q: %v",
				subreddit, len(postsCommentsAndUsers)+1, postsToScrape, post.Title, post.ID, post.Author, err,
			)
			continue
		}
		postCommentsAndUser.User = user.(*reddit.User)
		log.INFO.Printf(
			"%q(%d/%d): OP for post %q (%s) is %q",
			subreddit, len(postsCommentsAndUsers)+1, postsToScrape, post.Title, post.ID,
			postCommentsAndUser.User.Name,
		)
		postsCommentsAndUsers = append(postsCommentsAndUsers, &postCommentsAndUser)
	}
	log.INFO.Printf("Gathered OPs and comments for %d/%d posts for subreddit %q", len(postsCommentsAndUsers), len(posts), subreddit)
	return
}

type redditSubredditScrapeResult struct {
	subreddit             string
	postsCommentsAndUsers []*PostCommentsAndUser
	err                   error
}

func redditSubredditScraper(jobs <-chan string, results chan<- redditSubredditScrapeResult) {
	for job := range jobs {
		result := redditSubredditScrapeResult{subreddit: job}
		result.postsCommentsAndUsers, result.err = SubredditFetch(job)
		results <- result
	}
}

type redditDiscoveryBatchJob struct {
	no        int
	subreddit string
	posts     PostIterable
}

type redditDiscoveryBatchResult struct {
	*redditDiscoveryBatchJob
	tempState *ScoutState
	err       error
}

func redditDiscoveryBatchWorker(
	jobs <-chan redditDiscoveryBatchJob,
	results chan<- redditDiscoveryBatchResult,
	gameScrapers *models.StorefrontScrapers[string],
) {
	for job := range jobs {
		result := redditDiscoveryBatchResult{
			redditDiscoveryBatchJob: &job,
			tempState:               StateInMemory(),
		}
		result.tempState.GetCachedField(StateType).SetOrAdd("Phase", Discovery)
		var subGameIDs mapset.Set[uuid.UUID]
		subGameIDs, result.err = DiscoveryBatch(job.no, job.posts, gameScrapers, result.tempState)
		result.tempState.GetIterableCachedField(GameIDsType).Merge(&GameIDs{subGameIDs})
		results <- result
	}
}

// RedditDiscoveryPhase will iterate over each subreddit in the config and find all the top posts for them. It will then
// iterate over each post found, and fetch a max of 100 comments for each post. From these comments, it will filter any
// out that are not the OP's. It will then look in the body of these comments (and posts) for a link to any game store
// pages.
//
// This can run in parallel to DiscoveryPhase, but should be passed a ScoutState that is in memory which can then be
// merged back into the main ScoutState.
func RedditDiscoveryPhase(state *ScoutState, gameScrapers *models.StorefrontScrapers[string], subreddits ...string) (err error) {
	if len(subreddits) == 0 {
		subreddits = globalConfig.Reddit.RedditSubreddits()
	}
	subredditJobs := make(chan string, len(subreddits))
	subredditResults := make(chan redditSubredditScrapeResult, len(subreddits))
	postJobs := make(chan redditDiscoveryBatchJob, len(subreddits))
	postResults := make(chan redditDiscoveryBatchResult, len(subreddits))

	for i := 0; i < globalConfig.Scrape.Constants.RedditSubredditScrapeWorkers; i++ {
		go redditSubredditScraper(subredditJobs, subredditResults)
	}

	for i := 0; i < globalConfig.Scrape.Constants.RedditDiscoveryBatchWorkers; i++ {
		go redditDiscoveryBatchWorker(postJobs, postResults, gameScrapers)
	}

	for _, subreddit := range subreddits {
		subredditJobs <- subreddit
	}
	close(subredditJobs)

	// Create a goroutine to consume all PostCommentsAndUsers for each subreddit and queue each one up to be transformed
	// into a Developer, DeveloperSnapshot, and a Game.
	go func() {
		for r := 0; r < len(subreddits); r++ {
			result := <-subredditResults
			if result.err != nil {
				log.ERROR.Printf("Subreddit %q could not be scraped: %v", result.subreddit, result.err)
				if !myErrors.IsTemporary(result.err) {
					err = errors.Wrap(result.err, "reddit scrape could not recover from non-temp error")
					return
				}
				continue
			}

			// Queue up all PostCommentsAndUser to be transformed into a Developer, DeveloperSnapshot, and a Game
			postJobs <- redditDiscoveryBatchJob{
				no:        r,
				subreddit: result.subreddit,
				posts:     Posts(result.postsCommentsAndUsers),
			}
		}

		// We can close subredditResults as we have processed them all
		close(subredditResults)
		// We can also close postJobs as we have queued up all posts that need to be transformed
		close(postJobs)
	}()

	// Consume all redditDiscoveryBatchResults from the redditDiscoveryBatchWorkers.
	for result := range postResults {
		if result.err != nil {
			log.ERROR.Printf(
				"%d posts for subreddit %q could not be DiscoveryBatched: %v",
				result.posts.Len(), result.subreddit, result.err,
			)
			if !myErrors.IsTemporary(result.err) {
				err = errors.Wrap(result.err, "reddit DiscoveryBatch-ing could not recover from non-temp error")
				return
			}
			continue
		}

		// MergeIterableCachedFields the gameIDs, userTweetTimes, and developerSnapshots from the result into
		// the state passed in (which should be an in memory state).
		log.INFO.Printf(
			"Merging gameIDs, userTweetTimes, and developerSnapshots from the result for subreddit %q, back into the "+
				"ScoutState for RedditDiscoveryPhase",
			result.subreddit,
		)
		state.MergeIterableCachedFields(result.tempState)

		// Then we add all the properties of the ScoutResult within the temp state in the result returned from
		// DiscoveryBatch.
		scoutResultAny, _ := result.tempState.GetCachedField(StateType).Get("Result")
		scoutResult := scoutResultAny.(*models.ScoutResult)
		log.INFO.Printf(
			"Merging temp ScoutResult properties collected from posts for subreddit %q back into the ScoutResult for RedditDiscoveryPhase: %#+v",
			result.subreddit, scoutResult.DiscoveryStats,
		)
		state.GetCachedField(StateType).SetOrAdd("Result", "DiscoveryStats", "Developers", models.SetOrAddAdd.Func(scoutResult.DiscoveryStats.Developers))
		state.GetCachedField(StateType).SetOrAdd("Result", "DiscoveryStats", "Games", models.SetOrAddAdd.Func(scoutResult.DiscoveryStats.Games))
		state.GetCachedField(StateType).SetOrAdd("Result", "DiscoveryStats", "TweetsConsumed", models.SetOrAddAdd.Func(scoutResult.DiscoveryStats.TweetsConsumed))
		state.GetCachedField(StateType).SetOrAdd("Result", "DiscoveryStats", "TotalSnapshots", models.SetOrAddAdd.Func(scoutResult.DiscoveryStats.TotalSnapshots))
	}
	close(postResults)
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
			Tweets(tweetRaw.TweetDictionaries()),
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
