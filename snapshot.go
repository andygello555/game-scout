package main

import (
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"sort"
	"time"
)

// SnapshotPhase aggregates the DeveloperSnapshots and UserTweetTimes into a single models.DeveloperSnapshot for each
// models.Developer and saves each aggregated snapshot to the DB.
func SnapshotPhase(state *ScoutState) (err error) {
	dsIter := state.GetIterableCachedField(DeveloperSnapshotsType).Iter()
	for dsIter.Continue() {
		idAny := dsIter.Key()
		snapshotsAny, _ := state.GetIterableCachedField(DeveloperSnapshotsType).Get(idAny)
		userTweetTimesAny, _ := state.GetIterableCachedField(UserTweetTimesType).Get(idAny)

		id := idAny.(string)
		snapshots := snapshotsAny.([]*models.DeveloperSnapshot)
		userTweetTimes := userTweetTimesAny.([]time.Time)

		// Find the developer so that we can set the TimesHighlighted field appropriately
		developer := models.Developer{}
		if err = db.DB.Find(&developer, id).Error; err != nil {
			log.ERROR.Printf("Could not find developer %s in the DB, TimesHighlighted will be set to 0", id)
		}

		aggregatedSnap := &models.DeveloperSnapshot{
			DeveloperID:                  id,
			Tweets:                       int32(len(userTweetTimes)),
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
		dsIter.Next()
	}
	return
}
