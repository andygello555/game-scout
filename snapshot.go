package main

import (
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/andygello555/gotils/v2/numbers"
	"github.com/andygello555/gotils/v2/slices"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"sort"
	"strings"
	"time"
)

// SnapshotPhase aggregates the DeveloperSnapshots and UserTweetTimes into a single models.DeveloperSnapshot for each
// models.Developer and saves each aggregated snapshot to the DB.
func SnapshotPhase(state *ScoutState) (err error) {
	state.GetCachedField(StateType).SetOrAdd("Result", "SnapshotStats", "Games", int64(state.GetIterableCachedField(GameIDsType).Len()))
	dsIter := MergeCachedFieldIterators(state.GetIterableCachedField(DeveloperSnapshotsType).Iter(), state.GetIterableCachedField(RedditDeveloperSnapshotsType).Iter())
	for dsIter.Continue() {
		var (
			userTimesType     CachedFieldType
			resultConsumedKey string
		)

		switch dsIter.Field().Type() {
		case DeveloperSnapshotsType:
			userTimesType, resultConsumedKey = UserTweetTimesType, "TweetsConsumed"
		case RedditDeveloperSnapshotsType:
			userTimesType, resultConsumedKey = RedditUserPostTimesType, "PostsConsumed"
		}

		idAny := dsIter.Key()
		snapshotsAny, _ := dsIter.Get()
		userTweetTimesAny, _ := state.GetIterableCachedField(userTimesType).Get(idAny)
		state.GetCachedField(StateType).SetOrAdd("Result", "SnapshotStats", "Developers", models.SetOrAddInc.Func())

		id := idAny.(string)
		snapshots := snapshotsAny.([]*models.DeveloperSnapshot)
		userTimes := userTweetTimesAny.([]time.Time)
		state.GetCachedField(StateType).SetOrAdd("Result", "SnapshotStats", "TotalSnapshots", models.SetOrAddAdd.Func(int64(len(snapshots))))
		state.GetCachedField(StateType).SetOrAdd("Result", "SnapshotStats", resultConsumedKey, models.SetOrAddAdd.Func(int64(len(userTimes))))

		// Find the developer so that we can set the TimesHighlighted field appropriately
		developer := models.Developer{}
		if err = db.DB.Find(&developer, "id = ?", id).Error; err != nil {
			log.ERROR.Printf("Could not find developer %s in the DB, TimesHighlighted will be set to 0", id)
		}

		aggregatedSnap := &models.DeveloperSnapshot{
			DeveloperID:                  id,
			Tweets:                       int32(len(userTimes)),
			TweetTimeRange:               models.NullDurationFromPtr(nil),
			LastTweetTime:                userTimes[0].UTC(),
			AverageDurationBetweenTweets: models.NullDurationFromPtr(nil),
			TimesHighlighted:             developer.TimesHighlighted,
		}

		switch dsIter.Field().Type() {
		case DeveloperSnapshotsType:
			aggregatedSnap.TweetIDs = []string{}
			aggregatedSnap.TweetsPublicMetrics = &twitter.TweetMetricsObj{}
			aggregatedSnap.UserPublicMetrics = &twitter.UserMetricsObj{}
			aggregatedSnap.ContextAnnotationSet = myTwitter.NewContextAnnotationSet()
		case RedditDeveloperSnapshotsType:
			aggregatedSnap.RedditPostIDs = []string{}
			aggregatedSnap.PostPublicMetrics = &models.RedditPostMetrics{}
			aggregatedSnap.RedditPublicMetrics = &models.RedditUserMetrics{}
		}

		// If we have more than one tweet/post, then we will calculate TweetTimeRange, and AverageDurationBetweenTweets
		if aggregatedSnap.Tweets > 1 {
			// Sort the tweet/post times for this user.
			sort.Slice(userTimes, func(i, j int) bool {
				return userTimes[i].Before(userTimes[j])
			})
			// Find the max and min times by looking at the head and end of array
			maxTime := userTimes[len(userTimes)-1]
			minTime := userTimes[0]
			aggregatedSnap.LastTweetTime = maxTime.UTC()
			// Aggregate the durations between all the tweets/posts
			betweenCount := time.Nanosecond * 0
			for t, tweetTime := range userTimes {
				// Aggregate the duration between this time and the last time
				if t > 0 {
					betweenCount += tweetTime.Sub(userTimes[t-1])
				}
			}
			timeRange := maxTime.Sub(minTime)
			aggregatedSnap.TweetTimeRange = models.NullDurationFromPtr(&timeRange)
			averageDurationBetween := time.Duration(int64(betweenCount) / int64(aggregatedSnap.Tweets))
			aggregatedSnap.AverageDurationBetweenTweets = models.NullDurationFromPtr(&averageDurationBetween)
		}

		// We then aggregate the Tweet/Reddit Post IDs, tweet/post/user public metrics, and context annotations for each
		// snapshot
		for _, snapshot := range snapshots {
			switch dsIter.Field().Type() {
			case DeveloperSnapshotsType:
				// Because we want tweet IDs to be unique, we will do a union of the TweetIDs of the aggregated snap and
				// the current snap, then convert back to a slice.
				aggregatedSnap.TweetIDs = mapset.NewThreadUnsafeSet[string](
					aggregatedSnap.TweetIDs...,
				).Union(
					mapset.NewThreadUnsafeSet[string](snapshot.TweetIDs...),
				).ToSlice()

				aggregatedSnap.TweetsPublicMetrics.Impressions += snapshot.TweetsPublicMetrics.Impressions
				aggregatedSnap.TweetsPublicMetrics.URLLinkClicks += snapshot.TweetsPublicMetrics.URLLinkClicks
				aggregatedSnap.TweetsPublicMetrics.UserProfileClicks += snapshot.TweetsPublicMetrics.UserProfileClicks
				aggregatedSnap.TweetsPublicMetrics.Likes += snapshot.TweetsPublicMetrics.Likes
				aggregatedSnap.TweetsPublicMetrics.Replies += snapshot.TweetsPublicMetrics.Replies
				aggregatedSnap.TweetsPublicMetrics.Retweets += snapshot.TweetsPublicMetrics.Retweets
				aggregatedSnap.TweetsPublicMetrics.Quotes += snapshot.TweetsPublicMetrics.Quotes

				aggregatedSnap.UserPublicMetrics.Followers = numbers.Max(aggregatedSnap.UserPublicMetrics.Followers, snapshot.UserPublicMetrics.Followers)
				aggregatedSnap.UserPublicMetrics.Following = numbers.Max(aggregatedSnap.UserPublicMetrics.Following, snapshot.UserPublicMetrics.Following)
				aggregatedSnap.UserPublicMetrics.Tweets = numbers.Max(aggregatedSnap.UserPublicMetrics.Tweets, snapshot.UserPublicMetrics.Tweets)
				aggregatedSnap.UserPublicMetrics.Listed = numbers.Max(aggregatedSnap.UserPublicMetrics.Listed, snapshot.UserPublicMetrics.Listed)

				it := snapshot.ContextAnnotationSet.Iterator()
				for elem := range it.C {
					aggregatedSnap.ContextAnnotationSet.Add(elem)
				}
			case RedditDeveloperSnapshotsType:
				// Find all unique pairs of subreddits and post IDs by first creating a new data type to store these
				// pairs.
				type pair struct {
					subreddit string
					postID    string
				}

				// Then we add all the pairs to a set of pairs
				redditPostPairSet := mapset.NewThreadUnsafeSet[pair]()
				joinedRedditPostIDs := slices.Join(aggregatedSnap.RedditPostIDs, snapshot.RedditPostIDs)
				for i := 0; i < len(joinedRedditPostIDs); i += 2 {
					subreddit := joinedRedditPostIDs[i]
					postID := joinedRedditPostIDs[i+1]
					redditPostPairSet.Add(pair{subreddit, postID})
				}

				// Finally, we create a new array to hold the unique pairs and iterate over the set and add each
				// component in the pair in turn.
				aggregatedSnap.RedditPostIDs = make([]string, 0)
				for p := range redditPostPairSet.Iterator().C {
					aggregatedSnap.RedditPostIDs = append(aggregatedSnap.RedditPostIDs, p.subreddit, p.postID)
				}

				aggregatedSnap.PostPublicMetrics.Ups += snapshot.PostPublicMetrics.Ups
				aggregatedSnap.PostPublicMetrics.Downs += snapshot.PostPublicMetrics.Downs
				aggregatedSnap.PostPublicMetrics.Score += snapshot.PostPublicMetrics.Score
				aggregatedSnap.PostPublicMetrics.UpvoteRatio += snapshot.PostPublicMetrics.UpvoteRatio
				aggregatedSnap.PostPublicMetrics.NumberOfComments += snapshot.PostPublicMetrics.NumberOfComments
				aggregatedSnap.PostPublicMetrics.SubredditSubscribers += snapshot.PostPublicMetrics.SubredditSubscribers

				aggregatedSnap.RedditPublicMetrics.PostKarma = numbers.Max(aggregatedSnap.RedditPublicMetrics.PostKarma, snapshot.RedditPublicMetrics.PostKarma)
				aggregatedSnap.RedditPublicMetrics.CommentKarma = numbers.Max(aggregatedSnap.RedditPublicMetrics.CommentKarma, snapshot.RedditPublicMetrics.CommentKarma)
			}
		}

		// Finally, create the aggregated snap and increment the SnapshotsCreated counter
		if err = db.DB.Create(aggregatedSnap).Error; err != nil {
			return errors.Wrapf(
				err, "could not save aggregation of %d %s for dev. %s",
				len(snapshots), dsIter.Field().Type().String(), id,
			)
		}

		state.GetCachedField(StateType).SetOrAdd("Result", "SnapshotStats", "SnapshotsCreated", models.SetOrAddInc.Func())
		log.INFO.Printf(
			"\tSaved %s %d/%d aggregation for %s which is comprised of %d partial snapshots",
			strings.TrimSuffix(dsIter.Field().Type().String(), "s"), dsIter.I()+1, dsIter.Len(), id, len(snapshots),
		)

		// Remember to advance the iterator
		dsIter.Next()
	}
	return
}
