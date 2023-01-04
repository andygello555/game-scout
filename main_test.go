package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/game-scout/email"
	myTwitter "github.com/andygello555/game-scout/twitter"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/volatiletech/null/v9"
	"gorm.io/gorm"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"
)

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

func clearDB(t *testing.T) {
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.Developer{}).Error; err != nil {
		t.Fatalf("Could not clearDB: %s", err.Error())
	}
}

var keepState = flag.Bool("keepState", false, "whether to keep the ScoutState for TestDiscoveryBatch and TestUpdatePhase")

func TestDiscoveryBatch(t *testing.T) {
	clearDB(t)
	const (
		sampleTweetsPath    = "samples/sampleTweets.json"
		finalDeveloperCount = 8
		finalGameCount      = 2
	)

	if sampleTweetsBytes, err := os.ReadFile(sampleTweetsPath); err != nil {
		t.Errorf("Cannot read sample tweets from %s: %s", sampleTweetsPath, err.Error())
	} else {
		var sampleTweets map[string]*twitter.TweetDictionary
		if err = json.Unmarshal(sampleTweetsBytes, &sampleTweets); err != nil {
			t.Errorf("Cannot unmarshal %s into map[string]*twitter.TweetDictionary: %s", sampleTweetsPath, err.Error())
		}

		// Create a brand new ScoutState
		var state *ScoutState
		if state, err = StateLoadOrCreate(true); err != nil {
			t.Errorf("Error occurred when creating ScoutState: %s", err.Error())
		}

		gameScrapers := models.NewStorefrontScrapers[string](
			globalConfig.Scrape, db.DB, 5, 4, len(sampleTweets)*maxGamesPerTweet,
			minScrapeStorefrontsForGameWorkerWaitTime, maxScrapeStorefrontsForGameWorkerWaitTime,
		)
		gameScrapers.Start()

		var subGameIDs mapset.Set[uuid.UUID]
		//if subGameIDs, err = DiscoveryBatch(1, sampleTweets, gameScrapeQueue, state); err != nil {
		if subGameIDs, err = DiscoveryBatch(1, sampleTweets, gameScrapers, state); err != nil {
			t.Errorf("Could not run DiscoveryBatch: %s", err.Error())
		}
		state.GetIterableCachedField(GameIDsType).Merge(&GameIDs{subGameIDs})

		if err = state.Save(); err != nil {
			t.Errorf("Error occurred when saving ScoutState: %s", err.Error())
		}

		// Cleanup
		gameScrapers.Wait()
		if !(*keepState) {
			state.Delete()
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

var developers = flag.Int("developers", 465, "the number of developers to update in TestUpdatePhase")

func TestUpdatePhase(t *testing.T) {
	var (
		scanner *bufio.Scanner
		err     error
	)
	clearDB(t)
	const (
		sampleDeveloperIDsPath = "samples/sampleUserIDs.txt"
		sampleGameWebsitesPath = "samples/sampleGameWebsites.txt"
		batchSize              = 100
		discoveryTweets        = 30250
	)

	closeFile := func(file *os.File, filename string) {
		if err = file.Close(); err != nil {
			t.Fatalf("Could not close %s: %s", filename, err.Error())
		}
	}

	// First we load all the game websites into an array
	gameWebsites := make([]null.String, 0)
	var gameWebsitesFile *os.File
	if gameWebsitesFile, err = os.Open(sampleGameWebsitesPath); err != nil {
		t.Fatalf("Cannot open %s: %s", sampleGameWebsitesPath, err.Error())
	}
	defer closeFile(gameWebsitesFile, sampleGameWebsitesPath)

	scanner = bufio.NewScanner(gameWebsitesFile)
	for scanner.Scan() {
		gameWebsites = append(gameWebsites, null.StringFrom(scanner.Text()))
	}

	// Then we open the sample file containing developer IDs and create a developer for each
	var developerIDsFile *os.File
	if developerIDsFile, err = os.Open(sampleDeveloperIDsPath); err != nil {
		t.Fatalf("Cannot open %s: %s", sampleDeveloperIDsPath, err.Error())
	}
	defer closeFile(developerIDsFile, sampleDeveloperIDsPath)

	// Create developers from the sampleDeveloperIDs file and also create a game and a snapshot for each.
	developerNo := 0
	developerSnapshotNo := 0
	sixDaysAgo := time.Now().UTC().Add(time.Hour * time.Duration(-24*6))
	scanner = bufio.NewScanner(developerIDsFile)
	for scanner.Scan() && developerNo != *developers {
		developer := models.Developer{ID: scanner.Text()}
		if err = db.DB.Create(&developer).Error; err != nil {
			t.Errorf("Cannot create Developer %s: %s", developer.ID, err.Error())
		}
		if testing.Verbose() {
			fmt.Printf("Created Developer no. %d: %s\n", developerNo+1, developer.ID)
		}

		if err = db.DB.Create(&models.Game{
			Name:        null.StringFrom(fmt.Sprintf("Game for Developer: %d", developerNo)),
			Storefront:  models.SteamStorefront,
			Website:     gameWebsites[developerNo],
			DeveloperID: developer.ID,
		}).Error; err != nil {
			t.Errorf(
				"Cannot create game %s for Developer %s: %s",
				gameWebsites[developerNo].String, developer.ID, err.Error(),
			)
		}
		developerNo++

		if err = db.DB.Create(&models.DeveloperSnapshot{
			Version:       0,
			DeveloperID:   developer.ID,
			Tweets:        1,
			LastTweetTime: sixDaysAgo,
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

	// Create a brand new ScoutState
	var state *ScoutState
	if state, err = StateLoadOrCreate(true); err != nil {
		t.Errorf("Error occurred when creating ScoutState: %s", err.Error())
	}
	state.GetCachedField(StateType).SetOrAdd("Phase", Update)
	state.GetCachedField(StateType).SetOrAdd("BatchSize", batchSize)
	state.GetCachedField(StateType).SetOrAdd("DiscoveryTweets", discoveryTweets)

	// Then we run the UpdatePhase function
	if err = UpdatePhase([]string{}, state); err != nil {
		t.Errorf("Error occurred in update phase: %s", err.Error())
	}

	if err = state.Save(); err != nil {
		t.Errorf("Error occurred when saving ScoutState: %s", err.Error())
	}

	if state.GetIterableCachedField(GameIDsType).Len() < developerNo {
		t.Errorf(
			"There are only %d gameIDs, expected >=%d",
			state.GetIterableCachedField(GameIDsType).Len(), developerNo,
		)
	}

	if !(*keepState) {
		state.Delete()
	}
}

func TestDisablePhase(t *testing.T) {
	var err error
	clearDB(t)
	rand.Seed(time.Now().Unix())
	const (
		extraDevelopers       = 100
		fakeDevelopers        = maxEnabledDevelopers + extraDevelopers
		minDeveloperSnapshots = 1
		maxDeveloperSnapshots = 10
	)

	expectedIDs := mapset.NewThreadUnsafeSet[int]()

	// To test the "disable" phase, we need to create some fake developers and snapshots for said developers. The
	// developers are created in a way that more "desirable" developers will be given a higher ID.
	previousWeightedScore := 0.0
	for d := 1; d <= int(math.Floor(fakeDevelopers)); d++ {
		developer := models.Developer{
			ID:          strconv.Itoa(d),
			Name:        fmt.Sprintf("Developer: %d", d),
			Username:    fmt.Sprintf("developer%d", d),
			Description: fmt.Sprintf("I am developer %d", d),
			PublicMetrics: &twitter.UserMetricsObj{
				Followers: d * 100,
				Following: d * 10,
				Tweets:    d * 200,
				Listed:    d * 2,
			},
		}
		if err = db.DB.Create(&developer).Error; err != nil {
			t.Errorf("Cannot create developer: \"%s\"", developer.Username)
		}

		if d <= extraDevelopers {
			expectedIDs.Add(d)
		}

		snaps := rand.Intn(maxDeveloperSnapshots-minDeveloperSnapshots+1) + minDeveloperSnapshots
		maxTweetTimeRange := float64(time.Hour) * 24.0 * (1.0 / float64(d))
		maxAverageDurationBetweenTweets := float64(time.Minute) * 30.0 * (1.0 / float64(d))
		maxTweetPublicMetrics := twitter.TweetMetricsObj{
			Impressions:       d * 2,
			URLLinkClicks:     d * 5,
			UserProfileClicks: d,
			Likes:             d * 10,
			Replies:           d * 3,
			Retweets:          d * 2,
			Quotes:            0,
		}
		fmt.Printf("Created developer: \"%s\" (%s). Creating %d snapshots for it:\n", developer.Username, developer.ID, snaps)
		for snap := 1; snap <= snaps; snap++ {
			wayThrough := float64(snap) / float64(snaps)
			developerSnap := models.DeveloperSnapshot{}
			developerSnap.DeveloperID = developer.ID
			developerSnap.Tweets = int32(d * 3)
			tweetTimeRange := time.Duration(maxTweetTimeRange * wayThrough)
			developerSnap.TweetTimeRange = models.NullDurationFromPtr(&tweetTimeRange)
			averageDurationBetweenTweets := time.Duration(maxAverageDurationBetweenTweets * wayThrough)
			developerSnap.AverageDurationBetweenTweets = models.NullDurationFromPtr(&averageDurationBetweenTweets)
			developerSnap.TweetsPublicMetrics = &twitter.TweetMetricsObj{
				Impressions:       int(float64(maxTweetPublicMetrics.Impressions) * wayThrough),
				URLLinkClicks:     int(float64(maxTweetPublicMetrics.URLLinkClicks) * wayThrough),
				UserProfileClicks: int(float64(maxTweetPublicMetrics.UserProfileClicks) * wayThrough),
				Likes:             int(float64(maxTweetPublicMetrics.Likes) * wayThrough),
				Replies:           int(float64(maxTweetPublicMetrics.Replies) * wayThrough),
				Retweets:          int(float64(maxTweetPublicMetrics.Retweets) * wayThrough),
				Quotes:            int(float64(maxTweetPublicMetrics.Quotes) * wayThrough),
			}
			developerSnap.UserPublicMetrics = &twitter.UserMetricsObj{
				Followers: int(float64(developer.PublicMetrics.Followers) * wayThrough),
				Following: int(float64(developer.PublicMetrics.Following) * wayThrough),
				Tweets:    int(float64(developer.PublicMetrics.Tweets) * wayThrough),
				Listed:    int(float64(developer.PublicMetrics.Listed) * wayThrough),
			}
			if err = db.DB.Create(&developerSnap).Error; err != nil {
				t.Errorf("Could not create developer snap no. %d for the developer: %s", snap, developer.Username)
			}

			if snap == snaps {
				if developerSnap.WeightedScore > previousWeightedScore {
					previousWeightedScore = developerSnap.WeightedScore
				} else {
					t.Errorf(
						"Newest snap for developer: \"%s\", has a weighted score that is less than the "+
							"previous developer's latest snap. %f >= %f",
						developer.Username, previousWeightedScore, developerSnap.WeightedScore,
					)
				}
			}

			fmt.Printf("\tCreated snapshot no. %d/%d for developer. Weighted score = %f\n", snap, snaps, developerSnap.WeightedScore)
		}
	}

	if err = DisablePhase(); err != nil {
		t.Errorf("Error was not expected to occur in DisablePhase: %s", err.Error())
	}

	// Check if there are exactly maxNonDisabledDevelopers
	var enabledDevelopers int64
	if err = db.DB.Model(&models.Developer{}).Where("NOT disabled").Count(&enabledDevelopers).Error; err != nil {
		t.Errorf("Could not get count of enabled developers: %s", err.Error())
	}

	if enabledDevelopers != int64(math.Floor(maxEnabledDevelopers)) {
		t.Errorf(
			"The number of enabled developers is not %d, it is %d",
			int(math.Floor(maxEnabledDevelopers)), enabledDevelopers,
		)
	}

	// Then we check if the 100 disabled developers have the IDs 1-100
	type developerID struct {
		ID string
	}
	var developerIDs []developerID
	if err = db.DB.Model(&models.Developer{}).Where("disabled").Find(&developerIDs).Error; err != nil {
		t.Errorf("Could not get IDs of disabled developers: %s", err.Error())
	}

	seenIDs := mapset.NewThreadUnsafeSet[int]()
	for _, id := range developerIDs {
		intID, _ := strconv.Atoi(id.ID)
		seenIDs.Add(intID)
	}

	if !seenIDs.Equal(expectedIDs) {
		t.Errorf(
			"The disabled developers aren't the first %d developers that were created. They are: %s",
			extraDevelopers, seenIDs.String(),
		)
	}
}

func ExampleStateLoadOrCreate() {
	var (
		state *ScoutState
		err   error
	)
	// Force the creation of a new ScoutState. This will remove any existing in-disk cache for ScoutStates for this day.
	if state, err = StateLoadOrCreate(true); err != nil {
		fmt.Printf("Could not create ScoutState: %v\n", err)
		return
	}
	// Add some items to be cached
	state.GetCachedField(UserTweetTimesType).SetOrAdd("1234", time.Now().UTC(), "12345", time.Now().UTC())
	state.GetCachedField(DeveloperSnapshotsType).SetOrAdd("1234", &models.DeveloperSnapshot{DeveloperID: "1234"})
	state.GetCachedField(GameIDsType).SetOrAdd(uuid.New(), uuid.New(), uuid.New())
	state.GetCachedField(DeletedDevelopersType).SetOrAdd(&email.TrendingDev{
		Developer: &models.Developer{
			ID:       "1234",
			Name:     "Deleted developer 1",
			Username: "dd1",
		},
		Snapshots: []*models.DeveloperSnapshot{
			{ID: uuid.New()},
			{ID: uuid.New()},
		},
		Games: []*models.Game{
			{ID: uuid.New()},
			{ID: uuid.New()},
		},
		Trend: &models.Trend{},
	})
	state.GetCachedField(StateType).SetOrAdd("Phase", Disable)
	// Save the ScoutState to disk
	if err = state.Save(); err != nil {
		fmt.Printf("Could not save ScoutState: %v\n", err)
		return
	}

	// Now we drop the reference to the old ScoutState, and load the previously created ScoutState from the disk
	state = nil
	if state, err = StateLoadOrCreate(false); err != nil {
		fmt.Printf("Could not create ScoutState: %v\n", err)
		return
	}
	fmt.Println("userTweetTimes:", state.GetIterableCachedField(UserTweetTimesType).Len())
	fmt.Println("developerSnapshots:", state.GetIterableCachedField(DeveloperSnapshotsType).Len())
	fmt.Println("gameIDs:", state.GetIterableCachedField(GameIDsType).Len())
	fmt.Println("deletedDevelopers:", state.GetIterableCachedField(DeletedDevelopersType).Len())
	deletedDeveloperOne, _ := state.GetIterableCachedField(DeletedDevelopersType).Get(0)
	fmt.Println("deletedDevelopers[0].Developer.ID:", deletedDeveloperOne.(*email.TrendingDev).Developer.ID)
	fmt.Println("deletedDevelopers[0].Snapshots:", len(deletedDeveloperOne.(*email.TrendingDev).Snapshots))
	fmt.Println("deletedDevelopers[0].Games:", len(deletedDeveloperOne.(*email.TrendingDev).Games))
	phase, _ := state.GetCachedField(StateType).Get("Phase")
	fmt.Println("phase:", phase)
	state.Delete()
	// Output:
	// userTweetTimes: 2
	// developerSnapshots: 1
	// gameIDs: 3
	// deletedDevelopers: 1
	// deletedDevelopers[0].Developer.ID: 1234
	// deletedDevelopers[0].Snapshots: 2
	// deletedDevelopers[0].Games: 2
	// phase: Disable
}

func TestCreateInitialSteamApps(t *testing.T) {
	if err := models.CreateInitialSteamApps(db.DB); err != nil {
		t.Errorf("Error occurred whilst running CreateInitialSteamApps: %v", err)
	}
}
