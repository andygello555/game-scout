package models

import (
	"encoding/json"
	"fmt"
	mapset "github.com/deckarep/golang-set/v2"
	"os"
	"strings"
	"testing"
	"time"
)

// We setup fake types that implement the TagConfig, StorefrontConfig, and ScrapeConfig.
type tagConfig struct {
	defaultValue     float64
	upvotesThreshold float64
	values           map[string]float64
}

func (tc *tagConfig) TagDefaultValue() float64      { return tc.defaultValue }
func (tc *tagConfig) TagUpvotesThreshold() float64  { return tc.upvotesThreshold }
func (tc *tagConfig) TagValues() map[string]float64 { return tc.values }

type storefrontConfig struct {
	storefront Storefront
	tags       *tagConfig
}

func (sfc *storefrontConfig) StorefrontStorefront() Storefront { return sfc.storefront }
func (sfc *storefrontConfig) StorefrontTags() TagConfig        { return sfc.tags }

type scrapeConfig struct {
	storefronts []*storefrontConfig
}

// duration that can JSON serialised/deserialised. To be used in configs.
type duration struct {
	time.Duration
}

func (d duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value)
		return nil
	case string:
		var err error
		d.Duration, err = time.ParseDuration(value)
		if err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("invalid duration")
	}
}

type scrapeConstants struct {
	TransformTweetWorkers                     int      `json:"transform_tweet_workers"`
	UpdateDeveloperWorkers                    int      `json:"update_developer_workers"`
	MaxUpdateTweets                           int      `json:"max_update_tweets"`
	SecondsBetweenDiscoveryBatches            duration `json:"seconds_between_discovery_batches"`
	SecondsBetweenUpdateBatches               duration `json:"seconds_between_update_batches"`
	MaxTotalDiscoveryTweetsDailyPercent       float64  `json:"max_total_discovery_tweets_daily_percent"`
	MaxEnabledDevelopersAfterEnablePhase      float64  `json:"max_enabled_developers_after_enable_phase"`
	MaxEnabledDevelopersAfterDisablePhase     float64  `json:"max_enabled_developers_after_disable_phase"`
	MaxDevelopersToEnable                     float64  `json:"max_developers_to_enable"`
	PercentageOfDisabledDevelopersToDelete    float64  `json:"percentage_of_disabled_developers_to_delete"`
	StaleDeveloperDays                        int      `json:"stale_developer_days"`
	DiscoveryGameScrapeWorkers                int      `json:"discovery_game_scrape_workers"`
	DiscoveryMaxConcurrentGameScrapeWorkers   int      `json:"discovery_max_concurrent_game_scrape_workers"`
	UpdateGameScrapeWorkers                   int      `json:"update_game_scrape_workers"`
	UpdateMaxConcurrentGameScrapeWorkers      int      `json:"update_max_concurrent_game_scrape_workers"`
	MinScrapeStorefrontsForGameWorkerWaitTime duration `json:"min_scrape_storefronts_for_game_worker_wait_time"`
	MaxScrapeStorefrontsForGameWorkerWaitTime duration `json:"max_scrape_storefronts_for_game_worker_wait_time"`
	MaxGamesPerTweet                          int      `json:"max_games_per_tweet"`
	MaxTrendWorkers                           int      `json:"max_trend_workers"`
	MaxTrendingDevelopers                     int      `json:"max_trending_developers"`
	MaxTopSteamApps                           int      `json:"max_top_steam_apps"`
	SockSteamAppScrapeWorkers                 int      `json:"sock_steam_app_scrape_workers"`
	SockMaxConcurrentSteamAppScrapeWorkers    int      `json:"sock_max_concurrent_steam_app_scrape_workers"`
	SockMaxSteamAppScraperJobs                int      `json:"sock_max_steam_app_scraper_jobs"`
	SockSteamAppScraperJobsPerMinute          int      `json:"sock_steam_app_scraper_jobs_per_minute"`
	SockSteamAppScraperDropJobsThreshold      int      `json:"sock_steam_app_scraper_drop_jobs_threshold"`
	SockMinSteamAppScraperWaitTime            duration `json:"sock_min_steam_app_scraper_wait_time"`
	SockMaxSteamAppScraperWaitTime            duration `json:"sock_max_steam_app_scraper_wait_time"`
	SockSteamAppScrapeConsumers               int      `json:"sock_steam_app_scrape_consumers"`
	SockDropperWaitTime                       duration `json:"sock_dropper_wait_time"`
	ScrapeMaxTries                            int      `json:"scrape_max_tries"`
	ScrapeMinDelay                            duration `json:"scrape_min_delay"`
}

func (c *scrapeConstants) maxTotalDiscoveryTweets() float64 {
	return 55000.0 * c.MaxTotalDiscoveryTweetsDailyPercent
}
func (c *scrapeConstants) maxTotalUpdateTweets() float64 {
	return 55000.0 * (1.0 - c.MaxTotalDiscoveryTweetsDailyPercent)
}
func (c *scrapeConstants) DefaultMaxTries() int           { return c.ScrapeMaxTries }
func (c *scrapeConstants) DefaultMinDelay() time.Duration { return c.ScrapeMinDelay.Duration }

func (sc *scrapeConfig) ScrapeConstants() ScrapeConstants {
	if fileBytes, err := os.ReadFile("../../config.json"); err != nil {
		panic(err)
	} else {
		constants := scrapeConstants{}
		if err = json.Unmarshal(fileBytes[3:], &constants); err != nil {
			panic(err)
		}
		return &constants
	}
}

func (sc *scrapeConfig) ScrapeStorefronts() []StorefrontConfig {
	storefronts := make([]StorefrontConfig, len(sc.storefronts))
	for i, storefront := range sc.storefronts {
		storefronts[i] = storefront
	}
	return storefronts
}

func (sc *scrapeConfig) ScrapeGetStorefront(storefront Storefront) (storefrontConfig StorefrontConfig) {
	found := false
	for _, storefrontConfig = range sc.storefronts {
		if storefrontConfig.StorefrontStorefront() == storefront {
			found = true
			break
		}
	}
	if !found {
		storefrontConfig = nil
	}
	return
}

func (sc *scrapeConfig) ScrapeDebug() bool { return true }

var fakeConfig = &scrapeConfig{storefronts: []*storefrontConfig{
	{
		storefront: SteamStorefront,
		tags: &tagConfig{
			defaultValue:     10,
			upvotesThreshold: 50,
			values:           map[string]float64{},
		},
	},
}}

func TestScrapeStorefrontsForGameWrapper(t *testing.T) {
	for i, test := range []struct {
		expectedName                       string
		expectedStorefront                 Storefront
		expectedWebsite                    string
		expectedPublisher                  string
		expectedVerifiedDeveloperUsernames mapset.Set[string]
		expectedDevelopers                 mapset.Set[string]
		expectedReleaseDate                time.Time
		gameModel                          GameModel[string]
		storefrontMapping                  map[Storefront]mapset.Set[string]
		fail                               bool
		isNil                              bool
	}{
		{
			expectedName:                       "Human: Fall Flat",
			expectedStorefront:                 SteamStorefront,
			expectedWebsite:                    "https://store.steampowered.com/app/477160",
			expectedPublisher:                  "Curve Games",
			expectedVerifiedDeveloperUsernames: mapset.NewThreadUnsafeSet[string]("curvegames"),
			expectedDevelopers:                 mapset.NewThreadUnsafeSet[string]("curvegames"),
			expectedReleaseDate:                time.Unix(1469206680, 0),
			gameModel:                          &Game{Developers: []string{"curvegames"}},
			storefrontMapping: map[Storefront]mapset.Set[string]{
				SteamStorefront: mapset.NewThreadUnsafeSet[string](
					"https://store.steampowered.com/app/477160",
					"https://store.steampowered.com/app/477160/Human_Fall_Flat/",
				),
			},
		},
		{
			expectedName:                       "Human: Fall Flat",
			expectedStorefront:                 SteamStorefront,
			expectedWebsite:                    "https://store.steampowered.com/app/477160",
			expectedPublisher:                  "Curve Games",
			expectedVerifiedDeveloperUsernames: mapset.NewThreadUnsafeSet[string](),
			expectedDevelopers:                 mapset.NewThreadUnsafeSet[string](),
			expectedReleaseDate:                time.Unix(1469206680, 0),
			gameModel:                          &Game{},
			storefrontMapping: map[Storefront]mapset.Set[string]{
				SteamStorefront: mapset.NewThreadUnsafeSet[string](
					"https://store.steampowered.com/app/477160",
				),
			},
		},
		{
			storefrontMapping: map[Storefront]mapset.Set[string]{
				SteamStorefront: mapset.NewThreadUnsafeSet[string](
					"https://store.steampowered.com/app/90000000",
				),
			},
			gameModel: &Game{},
			fail:      true,
		},
		{
			storefrontMapping: map[Storefront]mapset.Set[string]{},
			gameModel:         &Game{},
			fail:              true,
			isNil:             true,
		},
	} {
		gameModel := ScrapeStorefrontsForGameModel[string](test.gameModel, test.storefrontMapping, fakeConfig)
		if testing.Verbose() {
			fmt.Printf("%d: %#v\n", i, gameModel)
		}
		if !test.fail {
			for _, check := range []struct {
				name     string
				actual   string
				expected string
			}{
				{"name", gameModel.(*Game).Name.String, test.expectedName},
				{"storefront", string(gameModel.(*Game).Storefront), string(test.expectedStorefront)},
				{"website", gameModel.(*Game).Website.String, test.expectedWebsite},
				{"publisher", gameModel.(*Game).Publisher.String, test.expectedPublisher},
				{"verified_developer_usernames", strings.Join(gameModel.(*Game).VerifiedDeveloperUsernames, ","), strings.Join(test.expectedVerifiedDeveloperUsernames.ToSlice(), ",")},
				{"developers", strings.Join(gameModel.(*Game).Developers, ","), strings.Join(test.expectedDevelopers.ToSlice(), ",")},
				{"release_date", gameModel.(*Game).ReleaseDate.Time.String(), test.expectedReleaseDate.String()},
			} {
				if check.actual != check.expected {
					t.Errorf("%s: \"%s\" is not equal to \"%s\" on test no. %d", check.name, check.actual, check.expected, i)
				}
			}
		} else {
			if test.isNil {
				if gameModel.(*Game) != nil {
					t.Errorf("test no. %d resulted in a non-nil GameModel: %v", i, gameModel)
				}
			} else {
				if gameModel.(*Game).Name.IsValid() && gameModel.(*Game).Publisher.IsValid() {
					t.Errorf(
						"test no. %d did not fail as both Name (%s) and Publisher (%s) are set",
						i, gameModel.(*Game).Name.String, gameModel.(*Game).Publisher.String,
					)
				}
			}
		}
	}
}
