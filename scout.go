package main

import (
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/anaskhan96/soup"
	"github.com/andygello555/game-scout/db/models"
	myTwitter "github.com/andygello555/game-scout/twitter"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"github.com/volatiletech/null/v9"
	"time"
)

func transformTweet(tweet *twitter.TweetDictionary) (developer *models.Developer, developerSnap *models.DeveloperSnapshot, game *models.Game, tweetCreatedAt time.Time, err error) {
	developer = &models.Developer{}
	developerSnap = &models.DeveloperSnapshot{}
	game = &models.Game{
		Name:            null.StringFromPtr(nil),
		Website:         null.StringFromPtr(nil),
		Publisher:       null.StringFromPtr(nil),
		TotalReviews:    null.Int32FromPtr(nil),
		PositiveReviews: null.Int32FromPtr(nil),
		NegativeReviews: null.Int32FromPtr(nil),
		ReviewScore:     null.Float64FromPtr(nil),
	}
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
	game.DeveloperID = developer.ID
	developer.Name = tweet.Author.Name
	developer.Username = tweet.Author.UserName
	developer.PublicMetrics = tweet.Author.PublicMetrics
	developer.Description = tweet.Author.Description
	if developer.ProfileCreated, err = time.Parse(myTwitter.CreatedAtFormat, tweet.Author.CreatedAt); err != nil {
		err = errors.Wrap(err, "could not parse author's created_at")
		return
	}

	// Next we find if there are any SteamURLAppPages that are linked in the tweet
	urlSet := mapset.NewSet[string]()
	storefrontSet := mapset.NewSet[models.Storefront]()
	for _, url := range tweet.Tweet.Entities.URLs {
		for _, check := range []struct {
			url        ScrapeURL
			storefront models.Storefront
		}{
			{SteamAppPage, models.SteamStorefront},
		} {
			if check.url.Match(url.ExpandedURL) {
				urlSet.Add(url.ExpandedURL)
				storefrontSet.Add(check.storefront)
			}
		}
	}

	// If there are, then we visit the page and scrape some stats
	if storefrontSet.Contains(models.SteamStorefront) {
		storePageURL, _ := urlSet.Pop()
		var doc *soup.Root
		if doc, err = SteamAppPage.Soup(storePageURL); err == nil {
			if nameEl := doc.Find("div", "id", "appHubAppName"); nameEl.Error == nil {
				game.Name = null.StringFrom(nameEl.Text())
			}
			if devRows := doc.FindAll("div", "class", "dev_row"); len(devRows) > 0 {
				developerName := devRows[0].Text()
				publisherName := devRows[1].Text()
				if developerName != publisherName {
					game.Publisher = null.StringFrom(publisherName)
				}
			}
		}

		var json map[string]any
		if json, err = SteamAppReviews.JSON(477160, "*", "all", 20, "all", "all", -1, -1, "all"); err != nil {
			querySummary, ok := json["query_summary"].(map[string]any)
			if ok {
				game.TotalReviews = null.Int32From(int32(querySummary["total_reviews"].(float64)))
				game.PositiveReviews = null.Int32From(int32(querySummary["total_reviews"].(float64)))
				game.NegativeReviews = null.Int32From(int32(querySummary["total_reviews"].(float64)))
				game.ReviewScore = null.Float64From(float64(*game.PositiveReviews.Ptr()) / float64(*game.TotalReviews.Ptr()))
			}
		}

		game.Storefront = models.SteamStorefront
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
	userTweetCount := make(map[string]int)
	userTweetTimes := make(map[string][]time.Time)
	developerSnapshots := make(map[string]*models.DeveloperSnapshot)

	for i := 0; i < discoveryTweets; i += batchSize {
		var result myTwitter.BindingResult
		if result, err = myTwitter.Client.ExecuteBinding(myTwitter.RecentSearch, &myTwitter.BindingOptions{Total: batchSize}, query, opts); err != nil {
			log.ERROR.Printf("Could not fetch %d tweets in Scout on batch %d/%d: %s. Continuing...", batchSize, batchSize, discoveryTweets, err.Error())
			continue
		}

		// The sub-phase of the Discovery phase is the cleansing phase
		tweetRaw := result.Raw().(*twitter.TweetRaw)
		for _, tweet := range tweetRaw.TweetDictionaries() {
			var developer *models.Developer
			var developerSnap *models.DeveloperSnapshot
			var game *models.Game
			var tweetCreated time.Time
			if developer, developerSnap, game, tweetCreated, err = transformTweet(tweet); err == nil {
				userTweetCount[developer.ID] += 1
				if _, ok := userTweetTimes[developer.ID]; !ok {
					userTweetTimes[developer.ID] = make([]time.Time, 0)
				}
				userTweetTimes[developer.ID] = append(userTweetTimes[developer.ID], tweetCreated)
				// At this point we only save the developer and the game. We still need to aggregate all the possible
				// developerSnapshots
				if _, ok := developerSnapshots[developer.ID]; ok {
					// Create the developer
				}
				// Create the game
				// Add the developerSnap to the developerSnapshots
				developerSnapshots[developer.ID] = developerSnap
			}
		}

		// Set up the next batch of requests by finding the created at time of the oldest tweet from the current batch
		if opts.StartTime, err = time.Parse(myTwitter.CreatedAtFormat, tweetRaw.TweetDictionaries()[result.Meta().OldestID()].Tweet.CreatedAt); err != nil {
			log.ERROR.Printf("Could not parse CreatedAt in oldest tweet in Scout on batch %d/%d: %s. Continuing...", batchSize, discoveryTweets, err.Error())
			continue
		}
	}
	return
}
