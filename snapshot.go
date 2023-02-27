package main

import (
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/andygello555/gotils/v2/numbers"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"sort"
	"time"
)

// SnapshotPhase aggregates the DeveloperSnapshots and UserTweetTimes into a single models.DeveloperSnapshot for each
// models.Developer and saves each aggregated snapshot to the DB.
func SnapshotPhase(state *ScoutState) (err error) {
	state.GetCachedField(StateType).SetOrAdd("Result", "SnapshotStats", "Games", int64(state.GetIterableCachedField(GameIDsType).Len()))
	dsIter := state.GetIterableCachedField(DeveloperSnapshotsType).Iter()
	for dsIter.Continue() {
		idAny := dsIter.Key()
		snapshotsAny, _ := state.GetIterableCachedField(DeveloperSnapshotsType).Get(idAny)
		userTweetTimesAny, _ := state.GetIterableCachedField(UserTweetTimesType).Get(idAny)
		state.GetCachedField(StateType).SetOrAdd("Result", "SnapshotStats", "Developers", models.SetOrAddInc.Func())

		id := idAny.(string)
		snapshots := snapshotsAny.([]*models.DeveloperSnapshot)
		userTweetTimes := userTweetTimesAny.([]time.Time)
		state.GetCachedField(StateType).SetOrAdd("Result", "SnapshotStats", "TotalSnapshots", models.SetOrAddAdd.Func(int64(len(snapshots))))
		state.GetCachedField(StateType).SetOrAdd("Result", "SnapshotStats", "TweetsConsumed", models.SetOrAddAdd.Func(int64(len(userTweetTimes))))

		// Find the developer so that we can set the TimesHighlighted field appropriately
		developer := models.Developer{}
		if err = db.DB.Find(&developer, "id = ?", id).Error; err != nil {
			log.ERROR.Printf("Could not find developer %s in the DB, TimesHighlighted will be set to 0", id)
		}

		aggregatedSnap := &models.DeveloperSnapshot{
			DeveloperID:                  id,
			Tweets:                       int32(len(userTweetTimes)),
			TweetIDs:                     []string{},
			TweetTimeRange:               models.NullDurationFromPtr(nil),
			LastTweetTime:                userTweetTimes[0].UTC(),
			AverageDurationBetweenTweets: models.NullDurationFromPtr(nil),
			TweetsPublicMetrics:          &twitter.TweetMetricsObj{},
			UserPublicMetrics:            &twitter.UserMetricsObj{},
			ContextAnnotationSet:         myTwitter.NewContextAnnotationSet(),
			TimesHighlighted:             developer.TimesHighlighted,
		}

		// If we have more than one tweet, then we will calculate TweetTimeRange, and AverageDurationBetweenTweets
		if aggregatedSnap.Tweets > 1 {
			// Sort the tweet times for this user.
			sort.Slice(userTweetTimes, func(i, j int) bool {
				return userTweetTimes[i].Before(userTweetTimes[j])
			})
			// Find the max and min times by looking at the head and end of array
			maxTime := userTweetTimes[len(userTweetTimes)-1]
			minTime := userTweetTimes[0]
			aggregatedSnap.LastTweetTime = maxTime.UTC()
			// Aggregate the durations between all the tweets
			betweenCount := time.Nanosecond * 0
			for t, tweetTime := range userTweetTimes {
				// Aggregate the duration between this time and the last time
				if t > 0 {
					betweenCount += tweetTime.Sub(userTweetTimes[t-1])
				}
			}
			timeRange := maxTime.Sub(minTime)
			aggregatedSnap.TweetTimeRange = models.NullDurationFromPtr(&timeRange)
			averageDurationBetween := time.Duration(int64(betweenCount) / int64(aggregatedSnap.Tweets))
			aggregatedSnap.AverageDurationBetweenTweets = models.NullDurationFromPtr(&averageDurationBetween)
		}

		// We then aggregate the Tweet IDs, tweet/user public metrics, and context annotations for each snapshot
		for _, snapshot := range snapshots {
			// Because we want tweet IDs to be unique, we will do a union of the TweetIDs of the aggregated snap and the
			// current snap, then convert back to a slice.
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
		}
		if err = db.DB.Create(aggregatedSnap).Error; err != nil {
			return errors.Wrapf(err, "could not save aggregation of %d DeveloperSnapshots for dev. %s", len(snapshots), id)
		}
		state.GetCachedField(StateType).SetOrAdd("Result", "SnapshotStats", "SnapshotsCreated", models.SetOrAddInc.Func())
		log.INFO.Printf(
			"\tSaved DeveloperSnapshot %d/%d aggregation for %s which is comprised of %d partial snapshots",
			dsIter.I()+1, state.GetIterableCachedField(DeveloperSnapshotsType).Len(), id, len(snapshots),
		)
		dsIter.Next()
	}
	return
}
