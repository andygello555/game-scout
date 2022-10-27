package models

import (
	"database/sql/driver"
	"fmt"
	"github.com/anaskhan96/soup"
	"github.com/andygello555/game-scout/browser"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/volatiletech/null/v9"
	"strings"
	"time"
)

type Storefront string

const (
	UnknownStorefront Storefront = "U"
	SteamStorefront   Storefront = "S"
	ItchIOStorefront  Storefront = "I"
)

// String returns the formal name of the Storefront.
func (sf Storefront) String() string {
	switch sf {
	case UnknownStorefront:
		return "Unknown"
	case SteamStorefront:
		return "Steam"
	case ItchIOStorefront:
		return "itch.io"
	default:
		return "<nil>"
	}
}

func (sf *Storefront) Scan(value interface{}) error {
	*sf = Storefront(value.([]byte))
	return nil
}

func (sf Storefront) Value() (driver.Value, error) {
	return string(sf), nil
}

func (sf Storefront) Type() string {
	return "storefront_type"
}

func (sf Storefront) Values() []string {
	return []string{
		string(UnknownStorefront),
		string(SteamStorefront),
		string(ItchIOStorefront),
	}
}

func (sf Storefront) Storefronts() []Storefront {
	return []Storefront{
		UnknownStorefront,
		SteamStorefront,
		ItchIOStorefront,
	}
}

// ScrapeGame will fetch more info on the game located at the given URL. The scrape procedure depends on what Storefront
// the URL is for.
func (sf Storefront) ScrapeGame(url string, game *Game) {
	var err error
	switch sf {
	case UnknownStorefront:
		panic(fmt.Errorf("cannot scrape game on UnknownStorefront"))
	case SteamStorefront:
		appID := browser.SteamAppPage.ExtractArgs(url)[0]

		// Fetch the store page to gather info on the name and the publisher
		var doc *soup.Root
		if doc, err = browser.SteamAppPage.Soup(appID); err == nil {
			if nameEl := doc.Find("div", "id", "appHubAppName"); nameEl.Error == nil {
				game.Name = null.StringFrom(nameEl.Text())
			}
			if devRows := doc.FindAll("div", "class", "dev_row"); len(devRows) > 0 {
				developerName := strings.TrimSpace(devRows[0].Find("a").Text())
				publisherName := strings.TrimSpace(devRows[1].Find("a").Text())
				if developerName != publisherName {
					game.Publisher = null.StringFrom(publisherName)
				}
			}
		}

		// Fetch reviews to gather headline stats on the number of reviews
		var json map[string]any
		if json, err = browser.SteamAppReviews.JSON(appID, "*", "all", 20, "all", "all", -1, -1, "all"); err == nil {
			querySummary, ok := json["query_summary"].(map[string]any)
			if ok {
				game.TotalReviews = null.Int32From(int32(querySummary["total_reviews"].(float64)))
				game.PositiveReviews = null.Int32From(int32(querySummary["total_positive"].(float64)))
				game.NegativeReviews = null.Int32From(int32(querySummary["total_negative"].(float64)))
				game.ReviewScore = null.Float64From(float64(*game.PositiveReviews.Ptr()) / float64(*game.TotalReviews.Ptr()))
			}
		}

		// Finally, fetch all the community posts and aggregate the upvotes, downvotes, and comment totals
		const batchSize = 50
		const maxTries = 3
		gidEvent := "0"
		gidAnnouncement := "0"
		gids := mapset.NewSet[string]()
		tries := maxTries
		for {
			var jsonBody map[string]any
			if jsonBody, err = browser.SteamCommunityPosts.JSON(appID, 0, batchSize, gidEvent, gidAnnouncement); err != nil {
				if tries > 0 {
					tries--
					time.Sleep(time.Second * 2 * time.Duration(maxTries+1-tries))
					continue
				}
				break
			}
			eventsProcessed := 0

			// Once we know we can get some data we will set the totals fields
			if !game.TotalUpvotes.IsValid() || !game.TotalDownvotes.IsValid() || !game.TotalComments.IsValid() {
				game.TotalUpvotes = null.Int32From(0)
				game.TotalDownvotes = null.Int32From(0)
				game.TotalComments = null.Int32From(0)
			}

			for _, event := range jsonBody["events"].([]interface{}) {
				eventBody := event.(map[string]any)
				gid := eventBody["gid"].(string)
				gidEvent = gid
				if !gids.Contains(gid) {
					gids.Add(gid)
					announcementBody := eventBody["announcement_body"].(map[string]any)
					gidAnnouncement = announcementBody["gid"].(string)
					game.TotalUpvotes.Int32 += int32(announcementBody["voteupcount"].(float64))
					game.TotalDownvotes.Int32 += int32(announcementBody["votedowncount"].(float64))
					game.TotalComments.Int32 += int32(announcementBody["commentcount"].(float64))
					eventsProcessed++
				}
			}

			if eventsProcessed == 0 {
				break
			}
		}
	default:
		panic(fmt.Errorf("scraping procedure for Storefront %s has not yet been implemented", sf.String()))
	}
}

// ScrapeStorefrontsForGame will scrape all the storefront URLs found for a given game and collate more data on it that
// will be stored in a Game model instance.
func ScrapeStorefrontsForGame(storefrontsFound map[Storefront]mapset.Set[string]) (game *Game) {
	game = nil
	if len(storefrontsFound) > 0 {
		game = &Game{
			Storefront:      UnknownStorefront,
			Name:            null.StringFromPtr(nil),
			Website:         null.StringFromPtr(nil),
			Publisher:       null.StringFromPtr(nil),
			TotalReviews:    null.Int32FromPtr(nil),
			PositiveReviews: null.Int32FromPtr(nil),
			NegativeReviews: null.Int32FromPtr(nil),
			ReviewScore:     null.Float64FromPtr(nil),
			TotalUpvotes:    null.Int32FromPtr(nil),
			TotalDownvotes:  null.Int32FromPtr(nil),
			TotalComments:   null.Int32FromPtr(nil),
		}
		for _, storefront := range UnknownStorefront.Storefronts() {
			if _, ok := storefrontsFound[storefront]; ok {
				// Pop a random URL from the set of URLs for this storefront
				url, _ := storefrontsFound[SteamStorefront].Pop()
				// Do some scraping to find more details for the game
				storefront.ScrapeGame(url, game)
				// Finally, set the Storefront and Website fields
				game.Storefront = SteamStorefront
				game.Website = null.StringFrom(url)
			}
		}
	}
	return game
}
