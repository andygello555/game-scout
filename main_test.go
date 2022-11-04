package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	myTwitter "github.com/andygello555/game-scout/twitter"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
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

func clearDB(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.Developer{}).Error; err != nil {
		t.Fatalf("Could not clearDB: %s", err.Error())
	}
}

func TestDiscoveryBatch(t *testing.T) {
	clearDB(t)
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
		if _, err = DiscoveryBatch(1, sampleTweets, userTweetTimes, developerSnapshots); err != nil {
			t.Errorf("Could not run DiscoveryBatch: %s", err.Error())
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

func TestUpdatePhase(t *testing.T) {
	clearDB(t)
	const (
		sampleDeveloperIDsPath = "samples/sampleUserIDs.txt"
		batchSize              = 100
		discoveryTweets        = 30250
	)

	file, err := os.Open(sampleDeveloperIDsPath)
	if err != nil {
		t.Fatalf("Cannot open %s: %s", sampleDeveloperIDsPath, err.Error())
	}
	defer func(file *os.File) {
		err = file.Close()
		if err != nil {
			t.Fatalf("Could not close %s: %s", sampleDeveloperIDsPath, err.Error())
		}
	}(file)

	// Create developers from the sampleDeveloperIDs file and also create a snapshot for each.
	developerNo := 0
	developerSnapshotNo := 0
	sevenDaysAgo := time.Now().UTC().Add(time.Hour * time.Duration(-24*7))
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		developer := models.Developer{ID: scanner.Text()}
		if err = db.DB.Create(&developer).Error; err != nil {
			t.Errorf("Cannot create Developer %s: %s", developer.ID, err.Error())
		}
		developerNo++

		if err = db.DB.Create(&models.DeveloperSnapshot{
			Version:       0,
			DeveloperID:   developer.ID,
			Tweets:        1,
			LastTweetTime: sevenDaysAgo,
		}).Error; err != nil {
			t.Errorf("Cannot create DeveloperSnapshot for Developer %s: %s", developer.ID, err.Error())
		}
		developerSnapshotNo++
	}

	// Create the global Twitter client
	if err = myTwitter.ClientCreate(globalConfig.Twitter); err != nil {
		t.Fatalf("Could not create Twitter ClientWrapper: %s", err.Error())
	}

	// We also just run a RecentSearch request, so we can update the RateLimits of the myTwitter.ClientWrapper
	query := globalConfig.Twitter.TwitterQuery()
	if _, err = myTwitter.Client.ExecuteBinding(
		myTwitter.RecentSearch,
		&myTwitter.BindingOptions{Total: 10},
		query,
		twitter.TweetRecentSearchOpts{MaxResults: 10},
	); err != nil {
		t.Fatalf("Error occurred whilst making a test RecentSearch request: %s", err.Error())
	}

	// Then we run the UpdatePhase function
	userTweetTimes := make(map[string][]time.Time)
	developerSnapshots := make(map[string][]*models.DeveloperSnapshot)
	gameIDs := mapset.NewSet[uuid.UUID]()
	if err = UpdatePhase(batchSize, discoveryTweets, []string{}, userTweetTimes, developerSnapshots, gameIDs); err != nil {
		t.Errorf("Error occurred in update phase: %s", err.Error())
	}

	// We check if developerSnapshots and userTweetTimes have the same length as the number of developers read from the
	// sample file.
	if len(developerSnapshots) != developerNo {
		t.Errorf(
			"The number of developers in developerSnapshots does not equal the number of developer IDs "+
				"read/created from %s: %d vs. %d",
			sampleDeveloperIDsPath, len(developerSnapshots), developerNo,
		)
	}

	if len(userTweetTimes) != developerNo {
		t.Errorf(
			"The number of users in userTweetTimes does not equal the number of developer IDs "+
				"read/created from %s: %d vs. %d",
			sampleDeveloperIDsPath, len(userTweetTimes), developerNo,
		)
	}
}
