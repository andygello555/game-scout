package main

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myTwitter "github.com/andygello555/game-scout/twitter"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reflect"
	"sort"
	"time"
)

func TransformTweet(tweet *twitter.TweetDictionary) (developer *models.Developer, developerSnap *models.DeveloperSnapshot, game *models.Game, tweetCreatedAt time.Time, err error) {
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

	// Next we find if there are any SteamURLAppPages that are linked in the tweet
	storefrontMap := make(map[models.Storefront]mapset.Set[string])
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
				storefrontMap[check.storefront].Add(url.ExpandedURL)
			}
		}
	}

	// Scrape metrics from all the storefronts found for the game.
	game = models.ScrapeStorefrontsForGame(storefrontMap)
	if game != nil {
		game.DeveloperID = developer.ID
	}

	developerSnap.TweetsPublicMetrics = tweet.Tweet.PublicMetrics
	developerSnap.UserPublicMetrics = developer.PublicMetrics
	developerSnap.ContextAnnotationSet = myTwitter.NewContextAnnotationSet(tweet.Tweet.ContextAnnotations...)
	return
}

func ScoutBatch(batchNo int, dictionary map[string]*twitter.TweetDictionary, userTweetTimes map[string][]time.Time, developerSnapshots map[string][]*models.DeveloperSnapshot) (err error) {
	logPrefix := fmt.Sprintf("ScoutBatch %d: ", batchNo)
	updateOrCreateModel := func(value any) {
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

		// Then we construct the upsert clause
		if updateOrCreate = db.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns(db.GetModel(value).ColumnDBNames()),
		}); updateOrCreate.Error != nil {
			err = myTwitter.TemporaryWrapf(false, updateOrCreate.Error, "could not create update or create clause for %s", reflect.TypeOf(value).Elem().Name())
			return
		}

		// Finally, we create the model instance
		if after := updateOrCreate.Create(value); after.Error != nil {
			err = after.Error
		}
		log.INFO.Printf("%screated %s", logPrefix, reflect.TypeOf(value).Elem().Name())
	}

	log.INFO.Printf("Starting ScoutBatch no. %d", batchNo)
	tweetNo := 1
	for _, tweet := range dictionary {
		var developer *models.Developer
		var developerSnap *models.DeveloperSnapshot
		var game *models.Game
		var tweetCreated time.Time
		if developer, developerSnap, game, tweetCreated, err = TransformTweet(tweet); err == nil {
			log.INFO.Printf("%stransformed tweet no. %d successfully", logPrefix, tweetNo)
			if _, ok := userTweetTimes[developer.ID]; !ok {
				userTweetTimes[developer.ID] = make([]time.Time, 0)
			}
			userTweetTimes[developer.ID] = append(userTweetTimes[developer.ID], tweetCreated)
			// At this point we only save the developer and the game. We still need to aggregate all the possible
			// developerSnapshots
			if _, ok := developerSnapshots[developer.ID]; !ok {
				// Create/update the developer if a developer snapshot for it hasn't been added yet
				log.INFO.Printf("%sthis is the first time we have seen developer: %s (%s)", logPrefix, developer.Username, developer.ID)
				updateOrCreateModel(developer)
			}
			// Create the game
			updateOrCreateModel(game)
			if err != nil {
				return myTwitter.TemporaryWrap(false, err, "could not insert either Developer or Game into DB")
			}
			// Add the developerSnap to the developerSnapshots
			if _, ok := developerSnapshots[developer.ID]; !ok {
				developerSnapshots[developer.ID] = make([]*models.DeveloperSnapshot, 0)
			}
			developerSnapshots[developer.ID] = append(developerSnapshots[developer.ID], developerSnap)
			log.INFO.Printf("%sadded DeveloperSnapshot to mapping", logPrefix)
		} else {
			log.WARNING.Printf("%scouldn't transform tweet no. %d: %s. Skipping...", logPrefix, tweetNo, err.Error())
		}
		tweetNo++
	}
	return
}

func Scout(batchSize int, discoveryTweets int) (err error) {
	// 1st phase: Discovery. Search the recent tweets on Twitter belonging to the hashtags given in config.json.
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

	userTweetTimes := make(map[string][]time.Time)
	developerSnapshots := make(map[string][]*models.DeveloperSnapshot)
	batchNo := 1

	for i := 0; i < discoveryTweets; i += batchSize {
		var result myTwitter.BindingResult
		if result, err = myTwitter.Client.ExecuteBinding(myTwitter.RecentSearch, &myTwitter.BindingOptions{Total: batchSize}, query, opts); err != nil {
			log.ERROR.Printf("Could not fetch %d tweets in Scout on batch %d/%d: %s. Continuing...", batchSize, batchSize, discoveryTweets, err.Error())
			continue
		}

		// The sub-phase of the Discovery phase is the cleansing phase
		tweetRaw := result.Raw().(*twitter.TweetRaw)

		// Run ScoutBatch for this batch of tweet dictionaries
		// If the error is not temporary then we will kill the Scouting process
		if err = ScoutBatch(batchNo, tweetRaw.TweetDictionaries(), userTweetTimes, developerSnapshots); !myTwitter.IsTemporary(err) {
			return errors.Wrapf(err, "could not execute ScoutBatch for batch no. %d", batchNo)
		}

		// Set up the next batch of requests by finding the created at time of the oldest tweet from the current batch
		if opts.StartTime, err = time.Parse(myTwitter.CreatedAtFormat, tweetRaw.TweetDictionaries()[result.Meta().OldestID()].Tweet.CreatedAt); err != nil {
			log.ERROR.Printf("Could not parse CreatedAt in oldest tweet in Scout on batch %d/%d: %s. Continuing...", batchSize, discoveryTweets, err.Error())
			continue
		}
		batchNo++
	}

	// We then aggregate each developer snapshot for each developer into one main developer snapshot
	for id, snapshots := range developerSnapshots {
		aggregatedSnap := &models.DeveloperSnapshot{
			DeveloperID:                  id,
			Tweets:                       int32(len(userTweetTimes[id])),
			TweetTimeRange:               models.NullDurationFromPtr(nil),
			AverageDurationBetweenTweets: models.NullDurationFromPtr(nil),
			TweetsPublicMetrics:          &twitter.TweetMetricsObj{},
			UserPublicMetrics:            &twitter.UserMetricsObj{},
			ContextAnnotationSet:         myTwitter.NewContextAnnotationSet(),
		}

		// If we have more than one tweet, then we will calculate TweetTimeRange, and AverageDurationBetweenTweets
		if aggregatedSnap.Tweets > 1 {
			// Sort the tweet times for this user.
			sort.Slice(userTweetTimes[id], func(i, j int) bool {
				return userTweetTimes[id][i].Before(userTweetTimes[id][j])
			})
			// Find the max and min times by looking at the head and end of array
			maxTime := userTweetTimes[id][len(userTweetTimes[id])]
			minTime := userTweetTimes[id][0]
			// Aggregate the durations between all the tweets
			betweenCount := time.Nanosecond * 0
			for i, tweetTime := range userTweetTimes[id] {
				// Aggregate the duration between this time and the last time
				if i > 0 {
					betweenCount += tweetTime.Sub(userTweetTimes[id][i-1])
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
			aggregatedSnap.ContextAnnotationSet.Set = aggregatedSnap.ContextAnnotationSet.Union(snapshot.ContextAnnotationSet)
		}
	}
	return
}
