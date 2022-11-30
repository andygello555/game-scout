package models

import (
	mapset "github.com/deckarep/golang-set/v2"
	"strconv"
	"testing"
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
		expectedName              string
		expectedStorefront        Storefront
		expectedWebsite           string
		expectedPublisher         string
		expectedDeveloperVerified bool
		gameModel                 GameModel[string]
		storefrontMapping         map[Storefront]mapset.Set[string]
		fail                      bool
		isNil                     bool
	}{
		{
			expectedName:              "Human: Fall Flat",
			expectedStorefront:        SteamStorefront,
			expectedWebsite:           "https://store.steampowered.com/app/477160",
			expectedPublisher:         "Curve Games",
			expectedDeveloperVerified: true,
			gameModel:                 &Game{Developer: &Developer{Username: "curvegames"}},
			storefrontMapping: map[Storefront]mapset.Set[string]{
				SteamStorefront: mapset.NewThreadUnsafeSet[string](
					"https://store.steampowered.com/app/477160",
					"https://store.steampowered.com/app/477160/Human_Fall_Flat/",
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
				{"developer_verified", strconv.FormatBool(gameModel.(*Game).DeveloperVerified), strconv.FormatBool(test.expectedDeveloperVerified)},
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
