package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/game-scout/reddit"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/andygello555/gotils/v2/numbers"
	"github.com/andygello555/gotils/v2/slices"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
	"github.com/volatiletech/null/v9"
	"gorm.io/gorm"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"strings"
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
			globalConfig.Scrape, db.DB, 5, 4,
			len(sampleTweets)*globalConfig.Scrape.Constants.MaxGamesPerTweet,
			globalConfig.Scrape.Constants.MinScrapeStorefrontsForGameWorkerWaitTime.Duration,
			globalConfig.Scrape.Constants.MaxScrapeStorefrontsForGameWorkerWaitTime.Duration,
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
	expectedDeveloperIDs := mapset.NewSet[string]()
	for scanner.Scan() && developerNo != *developers {
		developer := models.Developer{ID: scanner.Text(), Username: strconv.Itoa(developerNo)}
		expectedDeveloperIDs.Add(developer.ID)
		if err = db.DB.Create(&developer).Error; err != nil {
			t.Errorf("Cannot create Developer %s: %s", developer.ID, err.Error())
		}
		if testing.Verbose() {
			t.Logf("Created Developer no. %d: %s\n", developerNo+1, developer.ID)
		}

		var storefront models.Storefront
		switch {
		case models.SteamStorefront.ScrapeURL().Match(gameWebsites[developerNo].String):
			storefront = models.SteamStorefront
		case models.ItchIOStorefront.ScrapeURL().Match(gameWebsites[developerNo].String):
			storefront = models.ItchIOStorefront
		default:
			storefront = models.UnknownStorefront
		}

		if err = db.DB.Create(&models.Game{
			Name:       null.StringFrom(fmt.Sprintf("Game for Developer: %d", developerNo)),
			Storefront: storefront,
			Website:    gameWebsites[developerNo],
			Developers: []string{developer.Username},
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
		var errorResponse *twitter.ErrorResponse
		if errors.As(err, &errorResponse) && strings.Contains(errorResponse.Error(), "429 Too Many Requests") {
			sleepBar(errorResponse.RateLimit.Reset.Time().UTC().Sub(time.Now().UTC()))
		} else {
			t.Fatalf("Error occurred whilst making a test RecentSearch request: %s", err.Error())
		}
	}

	// Create a brand new ScoutState
	state := StateInMemory()
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

	actualDeveloperIDs := mapset.NewSet[string]()
	iter := state.GetIterableCachedField(DeveloperSnapshotsType).Iter()
	for iter.Continue() {
		key := iter.Key()
		val, _ := iter.Get()
		fmt.Printf("%v: has %d snapshots\n", key, len(val.([]*models.DeveloperSnapshot)))
		actualDeveloperIDs.Add(key.(string))
		iter.Next()
	}
	fmt.Println("There are", state.GetIterableCachedField(DeveloperSnapshotsType).Len(), "DeveloperSnapshots")

	if !actualDeveloperIDs.Equal(expectedDeveloperIDs) {
		t.Errorf(
			"Developer's that were updated do not match the developer's that were queued. actual - expected = %v, expected - actual = %v",
			actualDeveloperIDs.Difference(expectedDeveloperIDs), expectedDeveloperIDs.Difference(actualDeveloperIDs),
		)
	}
}

func TestSnapshotPhase(t *testing.T) {
	clearDB(t)
	developer := models.Developer{
		ID:               "snapshotdevtest",
		Name:             "Snapshot Dev",
		Username:         "Snapshot",
		TimesHighlighted: 5,
	}
	if err := db.DB.Create(&developer).Error; err != nil {
		t.Errorf("Error was not expected to occur when creating %q test dev: %s", developer.ID, err.Error())
	}

	state := StateInMemory()
	tweetPublicMetrics := &twitter.TweetMetricsObj{
		Impressions:       10,
		URLLinkClicks:     10,
		UserProfileClicks: 10,
		Likes:             10,
		Replies:           10,
		Retweets:          10,
		Quotes:            10,
	}
	userPublicMetrics := &twitter.UserMetricsObj{
		Followers: 10,
		Following: 10,
		Tweets:    10,
		Listed:    10,
	}
	for i := 0; i < 5; i++ {
		state.GetIterableCachedField(UserTweetTimesType).SetOrAdd(developer.ID, time.Now().Add(time.Duration(i+1)*time.Hour*24*-1))
		state.GetIterableCachedField(DeveloperSnapshotsType).SetOrAdd(developer.ID, &models.DeveloperSnapshot{
			CreatedAt:            time.Now(),
			DeveloperID:          developer.ID,
			Tweets:               1,
			TweetIDs:             []string{strconv.Itoa(i)},
			TweetsPublicMetrics:  tweetPublicMetrics,
			UserPublicMetrics:    userPublicMetrics,
			ContextAnnotationSet: myTwitter.NewContextAnnotationSet(),
		})
	}

	if err := SnapshotPhase(state); err != nil {
		t.Errorf("Error was not expected to occur in SnapshotPhase: %s", err.Error())
	}

	if snapshots, err := developer.DeveloperSnapshots(db.DB); err != nil {
		t.Errorf("Error was not expected to occur when retrieving snapshots for %q: %s", developer.ID, err.Error())
	} else {
		if len(snapshots) != 1 {
			t.Errorf("There are %d snapshots for %q instead of 1", len(snapshots), developer.ID)
		} else {
			first := snapshots[0]
			for _, checkedMetric := range []struct {
				name     string
				actual   string
				expected string
			}{
				{"Tweets", strconv.Itoa(int(first.Tweets)), "5"},
				{
					name: "TweetIDs",
					actual: strconv.FormatBool(
						mapset.NewSet(
							slices.Comprehension[int, string](
								numbers.Range[int](0, 4, 1),
								func(idx int, value int, arr []int) string {
									return strconv.Itoa(value)
								},
							)...,
						).Equal(mapset.NewSet([]string(first.TweetIDs)...)),
					),
					expected: "true",
				},
				{"TweetsPublicMetrics", fmt.Sprintf("%v", *first.TweetsPublicMetrics), "{50 50 50 50 50 50 50}"},
				{"UserPublicMetrics", fmt.Sprintf("%v", *first.UserPublicMetrics), "{10 10 10 10}"},
				{"ContextAnnotationSet", first.ContextAnnotationSet.String(), "Set{}"},
				{"TimesHighlighted", strconv.Itoa(int(first.TimesHighlighted)), "5"},
			} {
				if checkedMetric.actual != checkedMetric.expected {
					t.Errorf(
						"Only snapshot for %q has %s = %s (%v) not %s like we were expecting",
						developer.ID, checkedMetric.name, checkedMetric.actual,
						reflect.ValueOf(first).Elem().FieldByName(checkedMetric.name).Interface(),
						checkedMetric.expected,
					)
				}
			}
		}
	}
}

const (
	extraDevelopers       = 200
	minDeveloperSnapshots = 1
	maxDeveloperSnapshots = 10
)

func createFakeDevelopersWithSnaps(t *testing.T, startDevelopers int, endDevelopers int, expectedOffset int) (expectedIDs mapset.Set[int]) {
	var err error
	expectedIDs = mapset.NewThreadUnsafeSet[int]()
	// To test the "disable" phase, we need to create some fake developers, snapshots, and games for said developers.
	// These are created in a way that more "desirable" developers will be given a higher ID.
	previousWeightedScore := 0.0
	bar := progressbar.Default(int64(endDevelopers - startDevelopers + 1))
	for d := startDevelopers; d <= endDevelopers; d++ {
		developer := models.Developer{
			ID:          strconv.Itoa(d),
			Name:        fmt.Sprintf("Developer: %d", d),
			Username:    fmt.Sprintf("developer%d", d),
			Description: fmt.Sprintf("I am developer %d", d),
			PublicMetrics: &twitter.UserMetricsObj{
				Followers: d * 100,
				Following: d * 10,
				Tweets:    100,
				Listed:    d * 2,
			},
		}
		if err = db.DB.Create(&developer).Error; err != nil {
			t.Errorf("Cannot create developer: \"%s\"", developer.Username)
		}

		if d <= expectedOffset {
			expectedIDs.Add(d)
		}

		games := int(numbers.ScaleRange(float64(d), float64(startDevelopers), float64(endDevelopers), 5.0, 1.0))
		if testing.Verbose() {
			fmt.Printf("Creating %d games for developer %s\n", games, developer.Username)
		}
		for g := 1; g <= games; g++ {
			game := models.Game{
				Name:       null.StringFrom(fmt.Sprintf("Game for Developer %d", d)),
				Storefront: models.SteamStorefront,
				Website: null.StringFrom(fmt.Sprintf(
					"https://store.steampowered.com/app/%sgame%d",
					developer.Username, g,
				)),
				Developers:                 []string{developer.Username},
				VerifiedDeveloperUsernames: []string{},
				ReleaseDate:                null.TimeFromPtr(nil),
				Publisher:                  null.StringFromPtr(nil),
				TotalReviews:               null.Int32From(0),
				PositiveReviews:            null.Int32From(0),
				NegativeReviews:            null.Int32From(0),
				TotalUpvotes:               null.Int32From(0),
				TotalDownvotes:             null.Int32From(0),
				TotalComments:              null.Int32From(0),
				TagScore:                   null.Float64From(0.0),
			}
			if err = db.DB.Create(&game).Error; err != nil {
				t.Errorf("Could not create game no. %d for the developer: %s", g, developer.Username)
			}
			if testing.Verbose() {
				fmt.Printf(
					"\tCreated game %s no. %d/%d for developer %s. Weighted score = %f\n",
					game.Website.String, g, games, developer.Username, game.WeightedScore.Float64,
				)
			}
		}

		snaps := rand.Intn(maxDeveloperSnapshots-minDeveloperSnapshots+1) + minDeveloperSnapshots
		maxTweetTimeRange := float64(time.Hour) * 24.0 * (float64(d) / (float64(endDevelopers) / 7))
		maxAverageDurationBetweenTweets := float64(time.Minute) * 30.0 * (float64(d) / (float64(endDevelopers) / 7))
		maxTweetPublicMetrics := twitter.TweetMetricsObj{
			Impressions:       d * 2,
			URLLinkClicks:     d * 5,
			UserProfileClicks: d,
			Likes:             d * 10,
			Replies:           d * 3,
			Retweets:          d * 2,
			Quotes:            0,
		}
		if testing.Verbose() {
			fmt.Printf("Created Developer %v. Creating %d snapshots for it:\n", developer, snaps)
			fmt.Printf("MaxTweetTimeRange = %f, MaxAverageDurationBetweenTweets = %f\n", maxTweetTimeRange, maxAverageDurationBetweenTweets)
		}
		for snap := 1; snap <= snaps; snap++ {
			wayThrough := float64(snap) / float64(snaps)
			developerSnap := models.DeveloperSnapshot{}
			developerSnap.DeveloperID = developer.ID
			developerSnap.Tweets = 10
			developerSnap.Games = 1
			tweetTimeRange := time.Duration(maxTweetTimeRange * wayThrough)
			developerSnap.TweetTimeRange = models.NullDurationFromPtr(&tweetTimeRange)
			averageDurationBetweenTweets := time.Duration(maxAverageDurationBetweenTweets * wayThrough)
			developerSnap.AverageDurationBetweenTweets = models.NullDurationFromPtr(&averageDurationBetweenTweets)
			if testing.Verbose() {
				fmt.Printf(
					"\tTweetTimeRange.Minutes = %f, AverageDurationBetweenTweets.Minutes = %f\n",
					tweetTimeRange.Minutes(), averageDurationBetweenTweets.Minutes(),
				)
			}
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
				Tweets:    developer.PublicMetrics.Tweets,
				Listed:    int(float64(developer.PublicMetrics.Listed) * wayThrough),
			}
			if err = db.DB.Create(&developerSnap).Error; err != nil {
				t.Errorf("Could not create developer snap no. %d for the developer: %s", snap, developer.Username)
			}

			if snap == snaps {
				if developerSnap.WeightedScore > previousWeightedScore || d == startDevelopers {
					previousWeightedScore = developerSnap.WeightedScore
				} else {
					t.Errorf(
						"Newest snap for developer: \"%s\", has a weighted score that is less than the "+
							"previous developer's latest snap. %f >= %f",
						developer.Username, previousWeightedScore, developerSnap.WeightedScore,
					)
				}
			}

			if testing.Verbose() {
				fmt.Printf(
					"\tCreated snapshot no. %d/%d for developer %s. Weighted score = %f\n",
					snap, snaps, developer.Username, developerSnap.WeightedScore,
				)
			}
		}
		_ = bar.Add(1)
	}
	return
}

func disablePhaseTest(t *testing.T) (state *ScoutState, expectedIDs mapset.Set[int]) {
	fakeDevelopers := int(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterDisablePhase + float64(extraDevelopers)))
	var err error
	rand.Seed(time.Now().Unix())

	expectedIDs = createFakeDevelopersWithSnaps(t, 1, fakeDevelopers, extraDevelopers)

	state = StateInMemory()
	if err = DisablePhase(state); err != nil {
		t.Errorf("Error was not expected to occur in DisablePhase: %s", err.Error())
	}
	return
}

func TestDisablePhase(t *testing.T) {
	var err error
	clearDB(t)

	state, expectedIDs := disablePhaseTest(t)

	// Check if there are exactly maxNonDisabledDevelopers
	var enabledDevelopers int64
	if err = db.DB.Model(&models.Developer{}).Where("NOT disabled").Count(&enabledDevelopers).Error; err != nil {
		t.Errorf("Could not get count of enabled developers: %s", err.Error())
	}

	if enabledDevelopers != int64(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterDisablePhase)) {
		t.Errorf(
			"The number of enabled developers is not %d, it is %d",
			int(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterDisablePhase)), enabledDevelopers,
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

	disabledDevelopers, _ := state.GetCachedField(StateType).Get("DisabledDevelopers")
	if disabledDevelopersNo := len(disabledDevelopers.([]string)); disabledDevelopersNo != extraDevelopers {
		t.Errorf(
			"List of IDs for the disabled developers does not equal %d (it equals %d)",
			extraDevelopers, disabledDevelopersNo,
		)
	}

	if !seenIDs.Equal(expectedIDs) {
		t.Errorf(
			"The disabled developers aren't the first %d developers that were created. They are: %s",
			extraDevelopers, seenIDs.String(),
		)
	}
}

func enablePhaseTest(t *testing.T) (state *ScoutState, enabledDeveloperCount int64) {
	var err error
	const additionalDisabledDevelopers = 500

	state, _ = disablePhaseTest(t)

	if err = db.DB.Model(&models.Developer{}).Where("not disabled").Count(&enabledDeveloperCount).Error; err != nil {
		t.Errorf("Error was not expected to occur whilst getting the count of the Developers table")
	} else {
		log.INFO.Printf("Enabled developers after Disable phase: %d", enabledDeveloperCount)
	}

	// Create 500 more developers to disable
	fakeDevelopers := int(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterDisablePhase + float64(extraDevelopers)))
	start, end := fakeDevelopers+1, fakeDevelopers+additionalDisabledDevelopers
	createdDevelopers := createFakeDevelopersWithSnaps(t, start, end, end)
	if err = db.DB.Model(
		&models.Developer{},
	).Where(
		"id IN ?",
		slices.Comprehension(createdDevelopers.ToSlice(), func(idx int, value int, arr []int) string {
			return strconv.Itoa(value)
		}),
	).Update("disabled", true).Error; err != nil {
		t.Errorf(
			"Error was not expected to occur whilst creating %d more disabled Developers",
			createdDevelopers.Cardinality(),
		)
	}

	if err = db.DB.Model(&models.Developer{}).Where("not disabled").Count(&enabledDeveloperCount).Error; err != nil {
		t.Errorf("Error was not expected to occur whilst getting the count of the Developers table")
	} else {
		log.INFO.Printf(
			"Enabled developers after creating %d more disabled Developers: %d",
			additionalDisabledDevelopers, enabledDeveloperCount,
		)
	}

	if err = EnablePhase(state); err != nil {
		t.Errorf("Error was not expected to occur in EnablePhase: %s", err.Error())
	}
	return
}

func TestEnablePhase(t *testing.T) {
	var err error
	clearDB(t)

	_, enabledDeveloperCount := enablePhaseTest(t)

	if err = db.DB.Model(&models.Developer{}).Where("not disabled").Count(&enabledDeveloperCount).Error; err != nil {
		t.Errorf("Error was not expected to occur whilst getting the final count of the Developers table")
	}

	if enabledDeveloperCount != int64(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterEnablePhase)) {
		t.Errorf(
			"The number of enabled developers after the Enable phase does not equal the expected value: %d (expected) vs. %d (actual)",
			int64(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterEnablePhase)), enabledDeveloperCount,
		)
	}
}

func TestDeletePhase(t *testing.T) {
	var err error
	clearDB(t)

	state, _ := enablePhaseTest(t)

	// Create a referenced SteamApp for each disabled developer
	var disabledDevelopersBefore []*models.Developer
	if err = db.DB.Where("disabled").Find(&disabledDevelopersBefore).Error; err != nil {
		t.Errorf("Error was not expected to occur whilst finding disabled developers: %s", err.Error())
	}
	expectedDevelopersDeleted := int(math.Floor(float64(len(disabledDevelopersBefore)) * globalConfig.Scrape.Constants.PercentageOfDisabledDevelopersToDelete))

	// Create a SteamApp for every disabled developer
	disabledDevApp := make(map[string]uint64)
	for i, disabledDeveloper := range disabledDevelopersBefore {
		app := models.SteamApp{
			ID:          uint64(i + 1),
			Name:        fmt.Sprintf("SteamApp for %s", disabledDeveloper.Username),
			DeveloperID: &disabledDeveloper.ID,
		}
		disabledDevApp[disabledDeveloper.ID] = app.ID
		if err = db.DB.Create(&app).Error; err != nil {
			t.Errorf("Could not create a SteamApp for developer %s: %v", disabledDeveloper.Username, err)
		}
	}

	// Run the DeletePhase
	if err = DeletePhase(state); err != nil {
		t.Errorf("Error was not expected to occur in DeletePhase: %s", err.Error())
	}

	// Check if the number of deleted developers is what we expected
	actualDevelopersDeleted := state.GetIterableCachedField(DeletedDevelopersType).Len()
	if actualDevelopersDeleted != expectedDevelopersDeleted {
		t.Errorf(
			"The number of developers deleted does not equal the number expected. %d vs %d",
			actualDevelopersDeleted, expectedDevelopersDeleted,
		)
	}

	// Check if the number of games for each deleted dev is 0, and if the related SteamApp has no developer referenced
	iter := state.GetIterableCachedField(DeletedDevelopersType).Iter()
	for iter.Continue() {
		deletedDev := iter.Key().(*models.TrendingDev)
		var gamesForDev int64
		if err = db.DB.Model(
			&models.Game{},
		).Where(
			"? = ANY(developers)",
			deletedDev.Developer.Username,
		).Count(&gamesForDev).Error; err != nil {
			t.Errorf(
				"Unexpected error whilst finding the number of games for deleted dev. %s: %v",
				deletedDev.Developer.Username, err,
			)
		} else {
			if gamesForDev != 0 {
				t.Errorf(
					"There is/are %d game(s) for deleted dev. %s that still exist",
					gamesForDev, deletedDev.Developer.Username,
				)
			}
		}

		var steamApp models.SteamApp
		if err = db.DB.Where(
			"id = ?",
			disabledDevApp[deletedDev.Developer.ID],
		).Find(&steamApp).Error; err != nil {
			t.Errorf(
				"Unexpected error whilst finding the SteamApp for deleted dev. %s: %v",
				deletedDev.Developer.Username, err,
			)
		} else {
			if steamApp.DeveloperID != nil {
				t.Errorf(
					"SteamApp %s (%d) is still referenced by developer %s",
					steamApp.Name, steamApp.ID, *steamApp.DeveloperID,
				)
			}
		}
		iter.Next()
	}

	var disabledDevelopersAfterCount int64
	if err = db.DB.Model(&models.Developer{}).Where("disabled").Count(&disabledDevelopersAfterCount).Error; err != nil {
		t.Errorf("Unexpected error whilst finding the new count of all the disabled developers: %v", err)
	}

	actualDevelopersDeleted = len(disabledDevelopersBefore) - int(disabledDevelopersAfterCount)
	if actualDevelopersDeleted != expectedDevelopersDeleted {
		t.Errorf(
			"%d (disabledDevelopersBefore) - %d (disabledDevelopersAfter) = %d. Expected %d",
			len(disabledDevelopersBefore), disabledDevelopersAfterCount, actualDevelopersDeleted,
			expectedDevelopersDeleted,
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
	resultAny, _ := state.GetCachedField(StateType).Get("Result")
	if err = resultAny.(*models.ScoutResult).DisableStats.Before(db.DB); err != nil {
		fmt.Printf("Error occurred: %v\n", err)
	}

	if err = db.DB.Create(&models.Developer{
		ID:       "1234",
		Name:     "Enabled Developer",
		Username: "enabled",
	}).Error; err != nil {
		fmt.Printf("Error occurred: %v\n", err)
	}

	if err = resultAny.(*models.ScoutResult).DisableStats.After(db.DB); err != nil {
		fmt.Printf("Error occurred: %v\n", err)
	}
	state.GetCachedField(StateType).SetOrAdd("Result", "Started", time.Now())
	state.GetCachedField(StateType).SetOrAdd("Result", "DiscoveryStats", "Developers", models.SetOrAddInc.Func())
	state.GetCachedField(StateType).SetOrAdd("Result", "DiscoveryStats", "Games", models.SetOrAddAdd.Func(int64(2)))
	state.GetCachedField(UserTweetTimesType).SetOrAdd("1234", time.Now().UTC(), "12345", time.Now().UTC())
	state.GetCachedField(DeveloperSnapshotsType).SetOrAdd("1234", &models.DeveloperSnapshot{DeveloperID: "1234"})
	state.GetCachedField(GameIDsType).SetOrAdd(uuid.New(), uuid.New(), uuid.New())
	state.GetCachedField(DeletedDevelopersType).SetOrAdd(&models.TrendingDev{
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
	resultAny, _ = state.GetCachedField(StateType).Get("Result")
	result := resultAny.(*models.ScoutResult)
	fmt.Println("state.Result.DiscoveryStats:", result.DiscoveryStats)
	fmt.Printf("state.Result.DisableStats: %+v\n", result.DisableStats)
	fmt.Println("userTweetTimes:", state.GetIterableCachedField(UserTweetTimesType).Len())
	fmt.Println("developerSnapshots:", state.GetIterableCachedField(DeveloperSnapshotsType).Len())
	fmt.Println("gameIDs:", state.GetIterableCachedField(GameIDsType).Len())
	fmt.Println("deletedDevelopers:", state.GetIterableCachedField(DeletedDevelopersType).Len())
	deletedDeveloperOne, _ := state.GetIterableCachedField(DeletedDevelopersType).Get(0)
	fmt.Println("deletedDevelopers[0].Developer.ID:", deletedDeveloperOne.(*models.TrendingDev).Developer.ID)
	fmt.Println("deletedDevelopers[0].Snapshots:", len(deletedDeveloperOne.(*models.TrendingDev).Snapshots))
	fmt.Println("deletedDevelopers[0].Games:", len(deletedDeveloperOne.(*models.TrendingDev).Games))
	phase, _ := state.GetCachedField(StateType).Get("Phase")
	fmt.Println("phase:", phase)
	state.Delete()
	// Output:
	// state.Result.DiscoveryStats: &{1 2 0 0 0}
	// state.Result.DisableStats: &{EnabledDevelopersBefore:0 DisabledDevelopersBefore:0 EnabledDevelopersAfter:1 DisabledDevelopersAfter:0 DeletedDevelopers:0 TotalSampledDevelopers:0}
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

func TestSubredditFetch(t *testing.T) {
	reddit.CreateClient(globalConfig.Reddit)
	const subreddit = "GameDevelopment"
	state := StateInMemory()

	if postsCommentsAndUsers, err := SubredditFetch(subreddit, state); err != nil {
		t.Errorf("Error occurred whilst fetching posts, comments, and users for top in %q: %v", subreddit, err)
	} else {
		var b []byte
		if b, err = json.MarshalIndent(postsCommentsAndUsers, "", "  "); err != nil {
			t.Errorf("Could not marshal []*PostCommentsAndUser to JSON: %v", err)
		}
		fmt.Println(string(b))
	}
}
