package tasks

import (
	"encoding/json"
	"fmt"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/g8rswimmer/go-twitter/v2"
	"log"
)

func TwitterClientTest(query string) (val string, err error) {
	opts := twitter.TweetRecentSearchOpts{
		Expansions: []twitter.Expansion{
			twitter.ExpansionEntitiesMentionsUserName,
			twitter.ExpansionAuthorID,
			twitter.ExpansionReferencedTweetsID,
			twitter.ExpansionReferencedTweetsIDAuthorID,
		},
		TweetFields: []twitter.TweetField{
			twitter.TweetFieldCreatedAt,
			twitter.TweetFieldConversationID,
			twitter.TweetFieldAttachments,
			twitter.TweetFieldPublicMetrics,
			twitter.TweetFieldOrganicMetrics,
			twitter.TweetFieldReferencedTweets,
		},
		SortOrder: twitter.TweetSearchSortOrderRelevancy,
	}
	var tweetResponse *twitter.TweetRecentSearchResponse
	if tweetResponse, err = myTwitter.Client.RecentSearch(query, opts, myTwitter.TweetsPerSecond); err != nil {
		return "", err
	}

	var enc []byte
	if enc, err = json.MarshalIndent(tweetResponse.Raw.TweetDictionaries(), "", "    "); err != nil {
		log.Panic(err)
	}
	fmt.Println(string(enc))

	var metaBytes []byte
	if metaBytes, err = json.MarshalIndent(tweetResponse.Meta, "", "    "); err != nil {
		log.Panic(err)
	}
	fmt.Println(string(metaBytes))
	return string(enc) + string(metaBytes), nil
}
