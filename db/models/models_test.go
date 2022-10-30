package models

import (
	mapset "github.com/deckarep/golang-set/v2"
	"testing"
)

func TestScrapeStorefrontsForGame(t *testing.T) {
	for i, test := range []struct {
		expectedName       string
		expectedStorefront Storefront
		expectedWebsite    string
		expectedPublisher  string
		storefrontMapping  map[Storefront]mapset.Set[string]
		fail               bool
	}{
		{
			expectedName:       "Human: Fall Flat",
			expectedStorefront: SteamStorefront,
			expectedWebsite:    "https://store.steampowered.com/app/477160",
			expectedPublisher:  "Curve Games",
			storefrontMapping: map[Storefront]mapset.Set[string]{
				SteamStorefront: mapset.NewSet[string](
					"https://store.steampowered.com/app/477160",
					"https://store.steampowered.com/app/477160/Human_Fall_Flat/",
				),
			},
		},
		{
			storefrontMapping: map[Storefront]mapset.Set[string]{
				SteamStorefront: mapset.NewSet[string](
					"https://store.steampowered.com/app/0",
				),
			},
			fail: true,
		},
	} {
		game := ScrapeStorefrontsForGame(test.storefrontMapping)
		if !test.fail {
			for _, check := range []struct {
				name     string
				actual   string
				expected string
			}{
				{"name", game.Name.String, test.expectedName},
				{"storefront", string(game.Storefront), string(test.expectedStorefront)},
				{"website", game.Website.String, test.expectedWebsite},
				{"publisher", game.Publisher.String, test.expectedPublisher},
			} {
				if check.actual != check.expected {
					t.Errorf("%s %s is not equal to %s on test no. %d", check.name, check.actual, check.expected, i)
				}
			}
		} else {
			if game.Name.IsValid() && game.Publisher.IsValid() {
				t.Errorf(
					"test no. %d did not fail as both Name (%s) and Publisher (%s) are set",
					i, game.Name.String, game.Publisher.String,
				)
			}
		}
	}
}
