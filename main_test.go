package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/game-scout/monday"
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
	"sync"
	"testing"
	"time"
)

func closeFile(t *testing.T, file *os.File, filename string) {
	if err := file.Close(); err != nil {
		t.Fatalf("Could not close %s: %s", filename, err.Error())
	}
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
		if subGameIDs, err = DiscoveryBatch(1, Tweets(sampleTweets), gameScrapers, state); err != nil {
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

	// First we load all the game websites into an array
	gameWebsites := make([]null.String, 0)
	var gameWebsitesFile *os.File
	if gameWebsitesFile, err = os.Open(sampleGameWebsitesPath); err != nil {
		t.Fatalf("Cannot open %s: %s", sampleGameWebsitesPath, err.Error())
	}
	defer closeFile(t, gameWebsitesFile, sampleGameWebsitesPath)

	scanner = bufio.NewScanner(gameWebsitesFile)
	for scanner.Scan() {
		gameWebsites = append(gameWebsites, null.StringFrom(scanner.Text()))
	}

	// Then we open the sample file containing developer IDs and create a developer for each
	var developerIDsFile *os.File
	if developerIDsFile, err = os.Open(sampleDeveloperIDsPath); err != nil {
		t.Fatalf("Cannot open %s: %s", sampleDeveloperIDsPath, err.Error())
	}
	defer closeFile(t, developerIDsFile, sampleDeveloperIDsPath)

	// Create developers from the sampleDeveloperIDs file and also create a game and a snapshot for each.
	developerNo := 0
	developerSnapshotNo := 0
	sixDaysAgo := time.Now().UTC().Add(time.Hour * time.Duration(-24*6))
	scanner = bufio.NewScanner(developerIDsFile)
	expectedDeveloperIDs := mapset.NewSet[string]()
	for scanner.Scan() && developerNo != *developers {
		line := scanner.Text()
		id, username := line, strconv.Itoa(developerNo)
		devType := models.TwitterDeveloperType
		if split := strings.Split(line, ","); len(split) > 1 {
			id, username = split[0], split[1]
			devType = models.RedditDeveloperType
		}

		developer := models.Developer{ID: id, Username: username, Type: devType}
		expectedDeveloperIDs.Add(developer.ID)
		if err = db.DB.Create(&developer).Error; err != nil {
			t.Errorf("Cannot create Developer %v: %s", developer, err.Error())
		}
		if testing.Verbose() {
			t.Logf("Created Developer no. %d: %v\n", developerNo+1, developer)
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
			Name:       null.StringFrom(fmt.Sprintf("Game for Developer %v", developer)),
			Storefront: storefront,
			Website:    gameWebsites[developerNo],
			Developers: []string{developer.TypedUsername()},
		}).Error; err != nil {
			t.Errorf(
				"Cannot create game %s for Developer %v: %s",
				gameWebsites[developerNo].String, developer, err.Error(),
			)
		}
		developerNo++

		if err = db.DB.Create(&models.DeveloperSnapshot{
			Version:       0,
			DeveloperID:   developer.ID,
			Tweets:        1,
			LastTweetTime: sixDaysAgo,
		}).Error; err != nil {
			t.Errorf("Cannot create DeveloperSnapshot for Developer %v: %s", developer, err.Error())
		}
		developerSnapshotNo++
	}

	// Create the global Reddit API client
	reddit.CreateClient(globalConfig.Reddit)

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

	if testing.Verbose() {
		fmt.Printf("user tweet times: %v (%d)\n", state.GetIterableCachedField(UserTweetTimesType), state.GetIterableCachedField(UserTweetTimesType).Len())
		fmt.Printf("user post times: %v (%d)\n", state.GetIterableCachedField(RedditUserPostTimesType), state.GetIterableCachedField(RedditUserPostTimesType).Len())
		fmt.Printf("developer snapshots: %v (%d)\n", state.GetIterableCachedField(DeveloperSnapshotsType), state.GetIterableCachedField(DeveloperSnapshotsType).Len())
		fmt.Printf("reddit developer snapshots: %v (%d)\n", state.GetIterableCachedField(RedditDeveloperSnapshotsType), state.GetIterableCachedField(RedditDeveloperSnapshotsType).Len())
		fmt.Printf("game IDs: %v (%d)\n", state.GetIterableCachedField(GameIDsType), state.GetIterableCachedField(GameIDsType).Len())
		fmt.Printf("deleted developers: %v (%d)\n", state.GetIterableCachedField(DeletedDevelopersType), state.GetIterableCachedField(DeletedDevelopersType).Len())
		fmt.Printf("state: %#+v\n", state.GetCachedField(StateType))
		result, _ := state.GetCachedField(StateType).Get("Result")
		fmt.Printf("scout result %#+v\n", result)
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
	iter := MergeCachedFieldIterators(state.GetIterableCachedField(DeveloperSnapshotsType).Iter(), state.GetIterableCachedField(RedditDeveloperSnapshotsType).Iter())
	for iter.Continue() {
		key := iter.Key()
		val, _ := iter.Get()
		fmt.Printf("%v: has %d snapshots\n", key, len(val.([]*models.DeveloperSnapshot)))
		actualDeveloperIDs.Add(key.(string))
		iter.Next()
	}
	fmt.Println("There are", iter.Len(), "DeveloperSnapshots")

	if !actualDeveloperIDs.Equal(expectedDeveloperIDs) {
		t.Errorf(
			"Developer's that were updated do not match the developer's that were queued. actual - expected = %v, expected - actual = %v",
			actualDeveloperIDs.Difference(expectedDeveloperIDs), expectedDeveloperIDs.Difference(actualDeveloperIDs),
		)
	}
}

func TestSnapshotPhase(t *testing.T) {
	clearDB(t)
	const (
		developersNo = 50
		snapshotsNo  = 5
	)

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
	redditPostMetrics := &models.RedditPostMetrics{
		Ups:                  10,
		Downs:                10,
		Score:                10,
		UpvoteRatio:          10,
		NumberOfComments:     10,
		SubredditSubscribers: 10,
	}
	userPublicMetrics := &twitter.UserMetricsObj{
		Followers: 10,
		Following: 10,
		Tweets:    10,
		Listed:    10,
	}
	redditUserMetrics := &models.RedditUserMetrics{
		PostKarma:    10,
		CommentKarma: 10,
	}

	for developerNo := 0; developerNo < developersNo; developerNo++ {
		var (
			devType                     models.DeveloperType
			userTimesType, devSnapsType CachedFieldType
		)
		switch developerNo % 2 {
		case 0:
			devType, userTimesType, devSnapsType = models.TwitterDeveloperType, UserTweetTimesType, DeveloperSnapshotsType
		default:
			devType, userTimesType, devSnapsType = models.RedditDeveloperType, RedditUserPostTimesType, RedditDeveloperSnapshotsType
		}

		developer := models.Developer{
			ID:               fmt.Sprintf("snapshotdev%d", developerNo),
			Name:             "Snapshot Dev",
			Username:         "Snapshot",
			Type:             devType,
			TimesHighlighted: 5,
		}
		if err := db.DB.Create(&developer).Error; err != nil {
			t.Errorf(
				"Error was not expected to occur when creating %v %s test dev: %s",
				developer, devType.String(), err.Error(),
			)
		}

		if testing.Verbose() {
			t.Logf("%d: Created %s Developer %v", developerNo+1, devType.String(), developer)
		}

		for snapshotNo := 0; snapshotNo < snapshotsNo; snapshotNo++ {
			state.GetIterableCachedField(userTimesType).SetOrAdd(developer.ID, time.Now().UTC().Add(time.Duration(snapshotNo+1)*time.Hour*24*-1))
			state.GetIterableCachedField(devSnapsType).SetOrAdd(developer.ID, &models.DeveloperSnapshot{
				CreatedAt:            time.Now().UTC(),
				DeveloperID:          developer.ID,
				Tweets:               1,
				TweetIDs:             []string{strconv.Itoa(snapshotNo)},
				RedditPostIDs:        []string{globalConfig.Reddit.RedditSubreddits()[0], strconv.Itoa(snapshotNo)},
				TweetsPublicMetrics:  tweetPublicMetrics,
				PostPublicMetrics:    redditPostMetrics,
				UserPublicMetrics:    userPublicMetrics,
				RedditPublicMetrics:  redditUserMetrics,
				ContextAnnotationSet: myTwitter.NewContextAnnotationSet(),
			})

			if testing.Verbose() {
				t.Logf("\t%d: Added snapshot for Developer %v", snapshotsNo+1, developer)
			}
		}
	}

	if err := SnapshotPhase(state); err != nil {
		t.Errorf("Error was not expected to occur in SnapshotPhase: %s", err.Error())
	}

	for developerNo := 0; developerNo < developersNo; developerNo++ {
		var developer models.Developer
		developerID := fmt.Sprintf("snapshotdev%d", developerNo)
		if err := db.DB.Find(&developer, "id = ?", developerID).Error; err != nil {
			t.Errorf("Error occurred whilst fetching developer of ID %q: %v", developerID, err)
		}

		if snapshots, err := developer.DeveloperSnapshots(db.DB); err != nil {
			t.Errorf("Error was not expected to occur when retrieving snapshots for %v: %s", developer, err.Error())
		} else {
			if len(snapshots) != 1 {
				t.Errorf("There are %d snapshots for %v instead of 1", len(snapshots), developer)
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
								slices.Comprehension(
									numbers.Range(0, snapshotsNo-1, 1),
									func(idx int, value int, arr []int) string {
										return strconv.Itoa(value)
									},
								)...,
							).Equal(mapset.NewSet([]string(first.TweetIDs)...)),
						),
						expected: map[bool]string{
							false: "false",
							true:  "true",
						}[developer.Type == models.TwitterDeveloperType],
					},
					{
						name: "RedditPostIDs",
						actual: strconv.FormatBool(
							slices.SameElements(
								slices.Comprehension(
									first.RedditPostIDs,
									func(idx int, value string, arr []string) any {
										return value
									},
								),
								[]any{"GameDevelopment", 0, "GameDevelopment", 1, "GameDevelopment", 2, "GameDevelopment", 3, "GameDevelopment", 4},
							),
						),
						expected: map[bool]string{
							false: "false",
							true:  "true",
						}[developer.Type == models.RedditDeveloperType],
					},
					{
						"TweetsPublicMetrics",
						fmt.Sprintf("%v", *first.TweetsPublicMetrics),
						map[bool]string{
							false: "{0 0 0 0 0 0 0}",
							true:  "{50 50 50 50 50 50 50}",
						}[developer.Type == models.TwitterDeveloperType],
					},
					{
						"PostPublicMetrics",
						fmt.Sprintf("%v", *first.PostPublicMetrics),
						map[bool]string{
							false: "{0 0 0 0 0 0}",
							true:  "{50 50 50 50 50 50}",
						}[developer.Type == models.RedditDeveloperType],
					},
					{
						"UserPublicMetrics",
						fmt.Sprintf("%v", *first.UserPublicMetrics),
						map[bool]string{
							false: "{0 0 0 0}",
							true:  "{10 10 10 10}",
						}[developer.Type == models.TwitterDeveloperType],
					},
					{
						"RedditPublicMetrics",
						fmt.Sprintf("%v", *first.RedditPublicMetrics),
						map[bool]string{
							false: "{0 0}",
							true:  "{10 10}",
						}[developer.Type == models.RedditDeveloperType],
					},
					{
						"ContextAnnotationSet",
						func() string {
							if first.ContextAnnotationSet == nil {
								return "nil"
							}
							return first.ContextAnnotationSet.String()
						}(),
						map[bool]string{
							false: "nil",
							true:  "Set{}",
						}[developer.Type == models.TwitterDeveloperType],
					},
					{"TimesHighlighted", strconv.Itoa(int(first.TimesHighlighted)), "5"},
				} {
					if checkedMetric.actual != checkedMetric.expected {
						t.Errorf(
							"Only snapshot for %v has %s = %s (%v) not %s like we were expecting",
							developer, checkedMetric.name, checkedMetric.actual,
							reflect.ValueOf(first).Elem().FieldByName(checkedMetric.name).Interface(),
							checkedMetric.expected,
						)
					}
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

// createFakeDevelopersWithSnaps creates endDevelopers - startDevelopers number of models.Developer(s) each with 1-5
// games and 1-10 models.DeveloperSnapshot(s). Each models.Developer is created with a sequential ID between
// startDevelopers+1 and endDevelopers. Created models.Developer's with a higher relative ID, will be more "desirable"
// (i.e have more snapshots with better weighted scores). If the current iteration is before the given expected offset
// then the current models.Developer's ID will be added to the expectedIDs set. If allTypes is true, then the actual
// created number of models.Developer(s) will be multiplied by the number of available models.DeveloperType(s). In this
// case, the models.DeveloperType of each created models.Developer will be decided using the following formula:
//
//	models.UnknownDeveloperType.Types()[d % len(models.UnknownDeveloperType.Types())]
//
// Where "d" is the current index of the iteration (zero-based).
func createFakeDevelopersWithSnaps(t *testing.T, startDevelopers int, endDevelopers int, expectedOffset int, allTypes bool) (expectedIDs mapset.Set[int]) {
	var err error
	expectedIDs = mapset.NewThreadUnsafeSet[int]()
	types := 1
	if allTypes {
		types = len(models.UnknownDeveloperType.Types())
	}
	startDevelopers, endDevelopers, expectedOffset = startDevelopers*types, endDevelopers*types, expectedOffset*types

	previousWeightedScore := 0.0
	bar := progressbar.Default(int64(endDevelopers - startDevelopers))
	for d := startDevelopers; d < endDevelopers; d++ {
		d1 := d + 1
		devType := models.UnknownDeveloperType.Types()[d%types]
		developer := models.Developer{
			ID:          strconv.Itoa(d1),
			Name:        fmt.Sprintf("Developer: %d", d1),
			Username:    fmt.Sprintf("developer%d", d1),
			Description: fmt.Sprintf("I am developer %d", d1),
			Type:        devType,
			PublicMetrics: &twitter.UserMetricsObj{
				Followers: d1 * 100,
				Following: d1 * 10,
				Tweets:    100,
				Listed:    d1 * 2,
			},
			RedditPublicMetrics: &models.RedditUserMetrics{
				PostKarma:    d1 * 100,
				CommentKarma: d1 * 200,
			},
		}
		if err = db.DB.Create(&developer).Error; err != nil {
			t.Errorf("Cannot create %s developer: %q", developer.Type, developer.String())
		}

		if d < expectedOffset {
			expectedIDs.Add(d1)
		}

		games := int(numbers.ScaleRange(float64(d1), float64(startDevelopers), float64(endDevelopers), 5.0, 1.0))
		if testing.Verbose() {
			fmt.Printf("Creating %d games for developer %q\n", games, developer.String())
		}
		for g := 1; g <= games; g++ {
			game := models.Game{
				Name:       null.StringFrom(fmt.Sprintf("Game for Developer %d", d1)),
				Storefront: []models.Storefront{models.SteamStorefront, models.ItchIOStorefront}[(g-1)%2],
				Website: []null.String{
					null.StringFrom(fmt.Sprintf(
						"https://store.steampowered.com/app/%sgame%d",
						developer.Username, g,
					)),
					null.StringFrom(browser.ItchIOGamePage.Fill(developer.Username, fmt.Sprintf("gameNo%d", g))),
				}[(g-1)%2],
				Developers:                 []string{developer.TypedUsername()},
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
				t.Errorf("Could not create game no. %d for the developer: %q", g, developer.String())
			}
			if testing.Verbose() {
				fmt.Printf(
					"\tCreated game %s no. %d/%d for developer %q. Weighted score = %f\n",
					game.Website.String, g, games, developer.String(), game.WeightedScore.Float64,
				)
			}
		}

		snaps := rand.Intn(maxDeveloperSnapshots-minDeveloperSnapshots+1) + minDeveloperSnapshots
		maxTweetTimeRange := float64(time.Hour) * 24.0 * (float64(d1) / (float64(endDevelopers) / 7))
		maxAverageDurationBetweenTweets := float64(time.Minute) * 30.0 * (float64(d1) / (float64(endDevelopers) / 7))
		maxTweetPublicMetrics := twitter.TweetMetricsObj{
			Impressions:       d1 * 2,
			URLLinkClicks:     d1 * 5,
			UserProfileClicks: d1,
			Likes:             d1 * 10,
			Replies:           d1 * 3,
			Retweets:          d1 * 2,
			Quotes:            0,
		}
		maxPostPublicMetrics := models.RedditPostMetrics{
			Ups:                  d1 * 10,
			Downs:                d1 * 5,
			Score:                d1 * 2,
			UpvoteRatio:          0,
			NumberOfComments:     d1 * 5,
			SubredditSubscribers: d1 * 2,
		}
		if testing.Verbose() {
			fmt.Printf("Created Developer %q. Creating %d snapshots for it:\n", developer.String(), snaps)
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
			developerSnap.PostPublicMetrics = &models.RedditPostMetrics{
				Ups:                  int(float64(maxPostPublicMetrics.Ups) * wayThrough),
				Downs:                int(float64(maxPostPublicMetrics.Downs) * wayThrough),
				Score:                int(float64(maxPostPublicMetrics.Score) * wayThrough),
				UpvoteRatio:          float32(float64(maxPostPublicMetrics.UpvoteRatio) * wayThrough),
				NumberOfComments:     int(float64(maxPostPublicMetrics.NumberOfComments) * wayThrough),
				SubredditSubscribers: int(float64(maxPostPublicMetrics.SubredditSubscribers) * wayThrough),
			}
			developerSnap.UserPublicMetrics = &twitter.UserMetricsObj{
				Followers: int(float64(developer.PublicMetrics.Followers) * wayThrough),
				Following: int(float64(developer.PublicMetrics.Following) * wayThrough),
				Tweets:    developer.PublicMetrics.Tweets,
				Listed:    int(float64(developer.PublicMetrics.Listed) * wayThrough),
			}
			developerSnap.RedditPublicMetrics = &models.RedditUserMetrics{
				PostKarma:    int(float64(developer.RedditPublicMetrics.PostKarma) * wayThrough),
				CommentKarma: int(float64(developer.RedditPublicMetrics.CommentKarma) * wayThrough),
			}

			if err = db.DB.Create(&developerSnap).Error; err != nil {
				t.Errorf("Could not create developer snap no. %d for the developer: %q", snap, developer.String())
			}

			if snap == snaps {
				if developerSnap.WeightedScore > previousWeightedScore || d == startDevelopers {
					previousWeightedScore = developerSnap.WeightedScore
				} else {
					t.Errorf(
						"Newest snap for developer: %q, has a weighted score that is less than the "+
							"previous developer's latest snap. %f >= %f",
						developer.String(), previousWeightedScore, developerSnap.WeightedScore,
					)
				}
			}

			if testing.Verbose() {
				fmt.Printf(
					"\tCreated snapshot no. %d/%d for developer %q. Weighted score = %f\n",
					snap, snaps, developer.String(), developerSnap.WeightedScore,
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
	rand.Seed(time.Now().UTC().Unix())

	expectedIDs = createFakeDevelopersWithSnaps(t, 0, fakeDevelopers, extraDevelopers, true)

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
	seenIDs := mapset.NewThreadUnsafeSet[int]()

	// Check if there are exactly maxNonDisabledDevelopers
	devTypes := models.UnknownDeveloperType.Types()
	for _, devType := range devTypes {
		var enabledDevelopers int64
		if err = db.DB.Model(&models.Developer{}).Where("NOT disabled AND type = ?", devType).Count(&enabledDevelopers).Error; err != nil {
			t.Errorf("Could not get count of enabled developers: %s", err.Error())
		}

		if enabledDevelopers != int64(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterDisablePhase)) {
			t.Errorf(
				"The number of enabled %s developers is not %d, it is %d",
				devType, int(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterDisablePhase)),
				enabledDevelopers,
			)
		}

		// Then we check if the 100 disabled developers have the IDs 1-100
		type developerID struct {
			ID string
		}
		var developerIDs []developerID
		if err = db.DB.Model(&models.Developer{}).Where("disabled AND type = ?", devType).Find(&developerIDs).Error; err != nil {
			t.Errorf("Could not get IDs of disabled %s developers: %s", devType, err.Error())
		}

		for _, id := range developerIDs {
			intID, _ := strconv.Atoi(id.ID)
			seenIDs.Add(intID)
		}

		disabledDevelopers, _ := state.GetCachedField(StateType).Get(string(devType) + "DisabledDevelopers")
		if disabledDevelopersNo := len(disabledDevelopers.([]models.DeveloperMinimal)); disabledDevelopersNo != extraDevelopers {
			t.Errorf(
				"List of IDs for %s disabled developers does not equal %d (it equals %d)",
				devType.String(), extraDevelopers, disabledDevelopersNo,
			)
		}
	}

	if !seenIDs.Equal(expectedIDs) {
		t.Errorf(
			"The disabled developers aren't the first %d developers that were created. They are: %s, vs. %s",
			extraDevelopers*len(devTypes), seenIDs.String(), expectedIDs.String(),
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

	// Create 500 more developers for each DeveloperType to disable
	noDevTypes := len(models.UnknownDeveloperType.Types())
	fakeDevelopers := int(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterDisablePhase + float64(extraDevelopers)))
	start, end := fakeDevelopers, fakeDevelopers+additionalDisabledDevelopers
	fmt.Println("start", start, "end", end, "fakeDevelopers", fakeDevelopers, "noDevTypes", noDevTypes)
	createdDevelopers := createFakeDevelopersWithSnaps(t, start, end, end, true)
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
	for _, devType := range models.UnknownDeveloperType.Types() {
		if err = db.DB.Model(&models.Developer{}).Where("not disabled AND type = ?", devType).Count(&enabledDeveloperCount).Error; err != nil {
			t.Errorf(
				"Error was not expected to occur whilst getting the final count of the %s Developers in the "+
					"Developers table",
				devType,
			)
		}

		if enabledDeveloperCount != int64(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterEnablePhase)) {
			t.Errorf(
				"The number of enabled %s developers after the Enable phase does not equal the expected "+
					"value: %d (expected) vs. %d (actual)",
				devType, int64(math.Floor(globalConfig.Scrape.Constants.MaxEnabledDevelopersAfterEnablePhase)),
				enabledDeveloperCount,
			)
		}
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
			deletedDev.Developer.TypedUsername(),
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
		Type:     models.TwitterDeveloperType,
		Name:     "Enabled Developer",
		Username: "enabled",
	}).Error; err != nil {
		fmt.Printf("Error occurred: %v\n", err)
	}

	if err = db.DB.Create(&models.Developer{
		ID:       "r1234",
		Type:     models.RedditDeveloperType,
		Name:     "Enabled Developer",
		Username: "renabled",
	}).Error; err != nil {
		fmt.Printf("Error occurred: %v\n", err)
	}

	if err = resultAny.(*models.ScoutResult).DisableStats.After(db.DB); err != nil {
		fmt.Printf("Error occurred: %v\n", err)
	}
	state.GetCachedField(StateType).SetOrAdd("Result", "Started", time.Now().UTC())
	state.GetCachedField(StateType).SetOrAdd("Result", "DiscoveryStats", "Developers", models.SetOrAddInc.Func())
	state.GetCachedField(StateType).SetOrAdd("Result", "DiscoveryStats", "Games", models.SetOrAddAdd.Func(int64(2)))
	state.GetCachedField(UserTweetTimesType).SetOrAdd("1234", time.Now().UTC(), "12345", time.Now().UTC())
	state.GetCachedField(RedditUserPostTimesType).SetOrAdd("r1234", time.Now().UTC(), "r12345", time.Now().UTC())
	state.GetCachedField(DeveloperSnapshotsType).SetOrAdd("1234", &models.DeveloperSnapshot{DeveloperID: "1234"})
	state.GetCachedField(RedditDeveloperSnapshotsType).SetOrAdd("r1234", &models.DeveloperSnapshot{DeveloperID: "r1234"})
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
	fmt.Println("redditUserPostTimes:", state.GetIterableCachedField(RedditUserPostTimesType).Len())
	fmt.Println("developerSnapshots:", state.GetIterableCachedField(DeveloperSnapshotsType).Len())
	fmt.Println("redditDeveloperSnapshots:", state.GetIterableCachedField(RedditDeveloperSnapshotsType).Len())
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
	// state.Result.DisableStats: &{EnabledDevelopersBefore:0 DisabledDevelopersBefore:0 EnabledDevelopersAfter:2 DisabledDevelopersAfter:0 DeletedDevelopers:0 TotalSampledDevelopers:0 TotalFinishedSamples:0}
	// userTweetTimes: 2
	// redditUserPostTimes: 2
	// developerSnapshots: 1
	// redditDeveloperSnapshots: 1
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

const testSubreddit = "GameDevelopment"

func TestSubredditFetch(t *testing.T) {
	reddit.CreateClient(globalConfig.Reddit)
	if postsCommentsAndUsers, err := SubredditFetch(testSubreddit); err != nil {
		t.Errorf("Error occurred whilst fetching posts, comments, and users for top in %q: %v", testSubreddit, err)
	} else {
		if len(postsCommentsAndUsers) > globalConfig.Scrape.Constants.RedditPostsPerSubreddit {
			t.Errorf(
				"There are %d posts, comments, and users returned by SubredditFetch for %q which exceeds the %d max",
				len(postsCommentsAndUsers), testSubreddit, globalConfig.Scrape.Constants.RedditPostsPerSubreddit,
			)
		}
	}
}

func TestRedditDiscoveryPhase(t *testing.T) {
	reddit.CreateClient(globalConfig.Reddit)
	state := StateInMemory()
	gameScrapers := models.NewStorefrontScrapers[string](
		globalConfig.Scrape, db.DB, 5, 4,
		globalConfig.Scrape.Constants.RedditPostsPerSubreddit*globalConfig.Scrape.Constants.MaxGamesPerTweet,
		globalConfig.Scrape.Constants.MinScrapeStorefrontsForGameWorkerWaitTime.Duration,
		globalConfig.Scrape.Constants.MaxScrapeStorefrontsForGameWorkerWaitTime.Duration,
	)
	gameScrapers.Start()

	var wg sync.WaitGroup
	wg.Add(1)
	if err := RedditDiscoveryPhase(&wg, state, gameScrapers, testSubreddit); err != nil {
		t.Errorf("Error occurred whilst performing discovery phase for subreddit %q: %v", testSubreddit, err)
	} else {
		if testing.Verbose() {
			fmt.Printf("user post times: %v (%d)\n", state.GetIterableCachedField(RedditUserPostTimesType), state.GetIterableCachedField(RedditUserPostTimesType).Len())
			fmt.Printf("developer snapshots: %v (%d)\n", state.GetIterableCachedField(RedditDeveloperSnapshotsType), state.GetIterableCachedField(RedditDeveloperSnapshotsType).Len())
			fmt.Printf("game IDs: %v (%d)\n", state.GetIterableCachedField(GameIDsType), state.GetIterableCachedField(GameIDsType).Len())
			fmt.Printf("deleted developers: %v (%d)\n", state.GetIterableCachedField(DeletedDevelopersType), state.GetIterableCachedField(DeletedDevelopersType).Len())
			fmt.Printf("state: %#+v\n", state.GetCachedField(StateType))
		}

		postTimes := state.GetIterableCachedField(RedditUserPostTimesType).Len()
		devSnaps := state.GetIterableCachedField(RedditDeveloperSnapshotsType).Len()
		if postTimes != devSnaps {
			t.Errorf(
				"The number of user times %d, does not match the number of developer snapshots %d",
				postTimes, devSnaps,
			)
		}

		if postTimes > globalConfig.Scrape.Constants.RedditPostsPerSubreddit {
			t.Errorf(
				"There are %d user post times, but we should have only scraped %d posts",
				postTimes, globalConfig.Scrape.Constants.RedditPostsPerSubreddit,
			)
		}

		if devSnaps > globalConfig.Scrape.Constants.RedditPostsPerSubreddit {
			t.Errorf(
				"There are %d dev snaps, but we should have only scraped %d posts",
				devSnaps, globalConfig.Scrape.Constants.RedditPostsPerSubreddit,
			)
		}
	}
}

func TestDiscoveryPhase(t *testing.T) {
	reddit.CreateClient(globalConfig.Reddit)
	if err := myTwitter.ClientCreate(globalConfig.Twitter); err != nil {
		t.Errorf("Could not setup Twitter client: %v", err)
	}
	clearDB(t)

	state := StateInMemory()
	state.GetCachedField(StateType).SetOrAdd("Phase", Discovery)
	state.GetCachedField(StateType).SetOrAdd("DiscoveryTweets", 200)
	state.GetCachedField(StateType).SetOrAdd("BatchSize", 100)

	if gameIDs, err := DiscoveryPhase(state); err != nil {
		t.Errorf("Error occurred whilst running DiscoveryPhase: %v", err)
	} else {
		if testing.Verbose() {
			fmt.Printf("user tweet times: %v (%d)\n", state.GetIterableCachedField(UserTweetTimesType), state.GetIterableCachedField(UserTweetTimesType).Len())
			fmt.Printf("user post times: %v (%d)\n", state.GetIterableCachedField(RedditUserPostTimesType), state.GetIterableCachedField(RedditUserPostTimesType).Len())
			fmt.Printf("developer snapshots: %v (%d)\n", state.GetIterableCachedField(DeveloperSnapshotsType), state.GetIterableCachedField(DeveloperSnapshotsType).Len())
			fmt.Printf("reddit developer snapshots: %v (%d)\n", state.GetIterableCachedField(RedditDeveloperSnapshotsType), state.GetIterableCachedField(RedditDeveloperSnapshotsType).Len())
			fmt.Printf("game IDs: %v (%d)\n", state.GetIterableCachedField(GameIDsType), state.GetIterableCachedField(GameIDsType).Len())
			fmt.Printf("returned game IDs: %v (%d)\n", gameIDs, gameIDs.Cardinality())
			fmt.Printf("deleted developers: %v (%d)\n", state.GetIterableCachedField(DeletedDevelopersType), state.GetIterableCachedField(DeletedDevelopersType).Len())
			fmt.Printf("state: %#+v\n", state.GetCachedField(StateType))
			result, _ := state.GetCachedField(StateType).Get("Result")
			fmt.Printf("scout result %#+v\n", result)
		}
	}
}

func TestMeasurePhase(t *testing.T) {
	var err error
	if globalConfig.Monday == nil {
		t.Fatalf("Cannot run TestMeasurePhase when there is no Monday config specified")
	}

	monday.CreateClient(globalConfig.Monday)
	if globalConfig.Monday.TestMapping != nil {
		if testing.Verbose() {
			t.Logf("Creating Monday API client using MondayConfig.TestMapping instead of MondayConfig.Mapping")
		}

		monday.CreateClient(&MondayConfig{
			Token:       globalConfig.Monday.Token,
			Mapping:     globalConfig.Monday.TestMapping,
			TestMapping: globalConfig.Monday.Mapping,
		})
	}

	clearDB(t)

	const (
		sampleGameWebsitesPath = "samples/sampleGameWebsites.txt"
		steamApps              = 20
	)

	_ = createFakeDevelopersWithSnaps(t, 0, 100, 100, true)

	// Add some games to the test Monday board
	var tailDevs []*models.Developer
	if err = db.DB.Where("id IN ?", slices.Comprehension[int, string](numbers.Range(74, 99, 1), func(idx int, value int, arr []int) string {
		return strconv.Itoa(value)
	})).Find(&tailDevs).Error; err != nil {
		t.Errorf("Could not fetch tail end of created fake developers: %v", err)
	}

	gameWebsites := make([]string, 0)
	var gameWebsitesFile *os.File
	if gameWebsitesFile, err = os.Open(sampleGameWebsitesPath); err != nil {
		t.Fatalf("Cannot open %s: %s", sampleGameWebsitesPath, err.Error())
	}
	defer closeFile(t, gameWebsitesFile, sampleGameWebsitesPath)

	scanner := bufio.NewScanner(gameWebsitesFile)
	for scanner.Scan() {
		gameWebsites = append(gameWebsites, scanner.Text())
	}

	// Make 20 fake SteamApps
	var previousWeightedScore null.Float64
	for i := 0; i < steamApps; i++ {
		appid := browser.SteamAppPage.ExtractArgs(gameWebsites[i])[0].(int64)
		steamApp := models.SteamApp{
			ID:                uint64(appid),
			Name:              fmt.Sprintf("SteamApp %d", appid),
			ReleaseDate:       time.Now().UTC().Add(time.Hour * 24 * time.Duration(i+1)),
			HasStorepage:      true,
			Publisher:         null.StringFromPtr(nil),
			TotalReviews:      0,
			PositiveReviews:   0,
			NegativeReviews:   0,
			ReviewScore:       0,
			TotalUpvotes:      int32(100 * (i + 1)),
			TotalDownvotes:    int32(50 * (i + 1)),
			TotalComments:     int32(100 * (i + 1)),
			TagScore:          10000.0 * (float64(i) + 1.0),
			AssetModifiedTime: time.Now().UTC().Add(time.Hour * 12 * time.Duration(steamApps-i) * -1),
		}

		if err = db.DB.Create(&steamApp).Error; err != nil {
			t.Errorf("Error occurred whilst creating fake SteamApp %q no. %d: %v", steamApp.Name, i+1, err)
		}

		if i == 0 {
			previousWeightedScore = steamApp.WeightedScore
		} else if steamApp.WeightedScore.Float64 < previousWeightedScore.Float64 {
			t.Errorf(
				"Weighted score for %s SteamApp (%f) is not greater than the previous SteamApp's weighted score: %f",
				numbers.Ordinal(i+1), steamApp.WeightedScore.Float64, previousWeightedScore.Float64,
			)
		}

		if testing.Verbose() {
			t.Logf("Created SteamApp %d/%d: %v", i+1, steamApps, steamApp)
		}
	}

	state := StateInMemory()
	state.GetCachedField(StateType).SetOrAdd("Phase", Measure)
	state.GetCachedField(StateType).SetOrAdd("DiscoveryTweets", 200)
	state.GetCachedField(StateType).SetOrAdd("BatchSize", 100)

	if err = MeasurePhase(state); err != nil {
		t.Errorf("Error occurred whilst running MeasurePhase: %v", err)
	}

	if testing.Verbose() {
		fmt.Printf("user tweet times: %v (%d)\n", state.GetIterableCachedField(UserTweetTimesType), state.GetIterableCachedField(UserTweetTimesType).Len())
		fmt.Printf("user post times: %v (%d)\n", state.GetIterableCachedField(RedditUserPostTimesType), state.GetIterableCachedField(RedditUserPostTimesType).Len())
		fmt.Printf("developer snapshots: %v (%d)\n", state.GetIterableCachedField(DeveloperSnapshotsType), state.GetIterableCachedField(DeveloperSnapshotsType).Len())
		fmt.Printf("reddit developer snapshots: %v (%d)\n", state.GetIterableCachedField(RedditDeveloperSnapshotsType), state.GetIterableCachedField(RedditDeveloperSnapshotsType).Len())
		fmt.Printf("game IDs: %v (%d)\n", state.GetIterableCachedField(GameIDsType), state.GetIterableCachedField(GameIDsType).Len())
		fmt.Printf("deleted developers: %v (%d)\n", state.GetIterableCachedField(DeletedDevelopersType), state.GetIterableCachedField(DeletedDevelopersType).Len())
		fmt.Printf("state: %#+v\n", state.GetCachedField(StateType))
		result, _ := state.GetCachedField(StateType).Get("Result")
		fmt.Printf("scout result %#+v\n", result)
	}
}
