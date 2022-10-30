package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/g8rswimmer/go-twitter/v2"
	"os"
	"sort"
	"testing"
	"time"
)

func ExampleScrapeURL_Match() {
	fmt.Println(browser.SteamAppPage.Match("http://store.steampowered.com/app/477160"))
	fmt.Println(browser.SteamAppPage.Match("http://store.steampowered.com/app/477160/Human_Fall_Flat/"))
	fmt.Println(browser.SteamAppPage.Match("https://store.steampowered.com/app/477160"))
	fmt.Println(browser.SteamAppPage.Match("https://store.steampowered.com/app/Human_Fall_Flat/"))
	// Output:
	// true
	// true
	// true
	// false
}

func ExampleScrapeURL_ExtractArgs() {
	fmt.Println(browser.SteamAppPage.ExtractArgs("http://store.steampowered.com/app/477160"))
	fmt.Println(browser.SteamAppPage.ExtractArgs("https://store.steampowered.com/app/477160/Human_Fall_Flat/"))
	// Output:
	// [477160]
	// [477160]
}

func ExampleScrapeURL_Soup() {
	fmt.Printf("Getting name of app 477160 from %s:\n", browser.SteamAppPage.Fill(477160))
	if soup, err := browser.SteamAppPage.Soup(477160); err != nil {
		fmt.Printf("Could not get soup for %s, because %s", browser.SteamAppPage.Fill(477160), err.Error())
	} else {
		fmt.Println(soup.Find("div", "id", "appHubAppName").Text())
	}
	// Output:
	// Getting name of app 477160 from https://store.steampowered.com/app/477160:
	// Human: Fall Flat
}

func ExampleScrapeURL_JSON() {
	args := []any{477160, "*", "all", 20, "all", "all", -1, -1, "all"}
	fmt.Printf("Getting review stats for 477160 from %s:\n", browser.SteamAppReviews.Fill(args...))
	if json, err := browser.SteamAppReviews.JSON(args...); err != nil {
		fmt.Printf("Could not get reviews for %s, because %s", browser.SteamAppReviews.Fill(args...), err.Error())
	} else {
		var i int
		keys := make([]string, len(json["query_summary"].(map[string]any)))
		for key := range json["query_summary"].(map[string]any) {
			keys[i] = key
			i++
		}
		sort.Strings(keys)
		fmt.Println(keys)
	}
	// Output:
	// Getting review stats for 477160 from https://store.steampowered.com/appreviews/477160?json=1&cursor=*&language=all&day_range=9223372036854775807&num_per_page=20&review_type=all&purchase_type=all&filter=all&start_date=-1&end_date=-1&date_range_type=all:
	// [num_reviews review_score review_score_desc total_negative total_positive total_reviews]
}

func TestMain(m *testing.M) {
	var keep = flag.Bool("keep", false, "keep test DB after tests")
	flag.Parse()
	dropTestDB := func() {
		if err := db.DropDB(globalConfig.DB.TestDBName(), globalConfig.DB); err != nil {
			panic(err)
		}
	}
	if !*keep {
		defer dropTestDB()
	}
	// First we have to delete the old test DB if it exists
	dropTestDB()
	if err := db.Open(globalConfig.DB); err != nil {
		panic(err)
	}
	m.Run()
	db.Close()
}

func TestScoutBatch(t *testing.T) {
	const sampleTweetsPath = "samples/sampleTweets.json"
	const finalDeveloperCount = 8
	const finalGameCount = 2

	if sampleTweetsBytes, err := os.ReadFile(sampleTweetsPath); err != nil {
		t.Errorf("Cannot read sample tweets from %s: %s", sampleTweetsPath, err.Error())
	} else {
		var sampleTweets map[string]*twitter.TweetDictionary
		if err = json.Unmarshal(sampleTweetsBytes, &sampleTweets); err != nil {
			t.Errorf("Cannot unmarshal %s into map[string]*twitter.TweetDictionary: %s", sampleTweetsPath, err.Error())
		}
		userTweetTimes := make(map[string][]time.Time)
		developerSnapshots := make(map[string][]*models.DeveloperSnapshot)
		if _, err = ScoutBatch(1, sampleTweets, userTweetTimes, developerSnapshots); err != nil {
			t.Errorf("Could not run ScoutBatch: %s", err.Error())
		}
	}

	// Check if the database contains the right things
	var err error
	var count int64
	if err = db.DB.Model(&models.Developer{}).Count(&count).Error; err != nil {
		t.Errorf("Could not count the number of developers in the DB: %s", err.Error())
	}
	if count != finalDeveloperCount {
		t.Errorf("There are %d Developers in the DB instead of %d", count, finalDeveloperCount)
	}

	if err = db.DB.Model(&models.Game{}).Count(&count).Error; err != nil {
		t.Errorf("Could not count the number of games in the DB: %s", err.Error())
	}
	if count != finalGameCount {
		t.Errorf("There are %d Games in the DB instead of %d", count, finalGameCount)
	}
}
