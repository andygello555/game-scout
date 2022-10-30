package models

import (
	"database/sql/driver"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/anaskhan96/soup"
	"github.com/andygello555/game-scout/browser"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	"github.com/volatiletech/null/v9"
	"reflect"
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
	switch value.(type) {
	case []byte:
		*sf = Storefront(value.([]byte))
	case string:
		*sf = Storefront(value.(string))
	default:
		panic(fmt.Errorf("could not convert DB value to Storefront"))
	}
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

// ScrapeGame will fetch more info on the given game located at the given URL. The scrape procedure depends on what
// Storefront the URL is for. This also returns a standardised URL for the game's webpage.
func (sf Storefront) ScrapeGame(url string, game *Game) (standardisedURL string) {
	var err error
	switch sf {
	case UnknownStorefront:
		panic(fmt.Errorf("cannot scrape game on UnknownStorefront"))
	case SteamStorefront:
		args := browser.SteamAppPage.ExtractArgs(url)
		appID := args[0]
		const maxTries = 3
		const minDelay = time.Second * 2

		// Fetch the store page to gather info on the name and the publisher
		if err = browser.SteamAppPage.TrySoup(maxTries, minDelay, func(doc *soup.Root) error {
			var nameEl soup.Root
			if nameEl = doc.Find("div", "id", "appHubAppName"); nameEl.Error != nil {
				return errors.Wrapf(nameEl.Error, "could not find name of game on SteamAppPage soup for %s", url)
			}
			game.Name = null.StringFrom(nameEl.Text())
			log.INFO.Printf("Game name (%d): %s", appID, game.Name.String)

			var devRows []soup.Root
			if devRows = doc.FindAll("div", "class", "dev_row"); len(devRows) < 2 {
				return fmt.Errorf("could not find publisher on SteamAppPage soup for %s, only found %d dev_rows", url, len(devRows))
			}

			developerName := strings.TrimSpace(devRows[0].Find("a").Text())
			publisherName := strings.TrimSpace(devRows[1].Find("a").Text())
			log.INFO.Printf("Developer name (%d): %s", appID, developerName)
			log.INFO.Printf("Publisher name (%d): %s", appID, publisherName)
			if developerName != publisherName {
				game.Publisher = null.StringFrom(publisherName)
			}
			return nil
		}, appID); err != nil {
			log.WARNING.Printf("Could not fetch info from SteamAppPage for %d: %s", appID, err.Error())
		}

		// Fetch reviews to gather headline stats on the number of reviews
		if err = browser.SteamAppReviews.TryJSON(maxTries, minDelay, func(jsonBody map[string]any) error {
			querySummary, ok := jsonBody["query_summary"].(map[string]any)
			if ok {
				game.TotalReviews = null.Int32From(int32(querySummary["total_reviews"].(float64)))
				game.PositiveReviews = null.Int32From(int32(querySummary["total_positive"].(float64)))
				game.NegativeReviews = null.Int32From(int32(querySummary["total_negative"].(float64)))
				log.INFO.Printf(
					"For game %d: total_reviews = %d, positive_reviews = %d, negative_reviews = %d",
					appID, game.TotalReviews.Int32, game.PositiveReviews.Int32, game.NegativeReviews.Int32,
				)
			} else {
				return fmt.Errorf("could not find query_summary key in SteamAppReviews response JSON")
			}
			return nil
		}, appID, "*", "all", 20, "all", "all", -1, -1, "all"); err != nil {
			log.WARNING.Printf(
				"Could not get SteamAppReviews JSON for %s: %s",
				browser.SteamAppReviews.Fill(appID, "*", "all", 20, "all", "all", -1, -1, "all"), err.Error(),
			)
		}

		// Finally, fetch all the community posts and aggregate the upvotes, downvotes, and comment totals
		const batchSize = 50
		gidEvent := "0"
		gidAnnouncement := "0"
		gids := mapset.NewSet[string]()
		tries := maxTries
		type returnType int
		const (
			con returnType = iota
			brk
		)

		for {
			rt := func() (rt returnType) {
				// Defer a closure to recover from any panics and set the appropriate return type
				defer func() {
					if pan := recover(); pan != nil {
						if tries > 0 {
							tries--
							time.Sleep(minDelay * time.Duration(maxTries+1-tries))
							rt = con
							return
						}
						err = fmt.Errorf("panic occurred: %v", pan)
						rt = brk
					}
				}()

				var jsonBody map[string]any
				if jsonBody, err = browser.SteamCommunityPosts.JSON(appID, 0, batchSize, gidEvent, gidAnnouncement); err != nil {
					log.WARNING.Printf("Could not get SteamCommunityPosts JSON for %s: %s. Tries left: %d", url, err.Error(), tries)
					if tries > 0 {
						tries--
						time.Sleep(minDelay * time.Duration(maxTries+1-tries))
						return con
					}
					return brk
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
					switch eventBody["gid"].(type) {
					case string:
						gidEvent = eventBody["gid"].(string)
					case float64:
						gidEvent = fmt.Sprintf("%f", eventBody["gid"].(float64))
					default:
						log.ERROR.Printf("Gid for event is %s not string or float64", reflect.TypeOf(eventBody["gid"]).String())
					}
					if !gids.Contains(gidEvent) {
						gids.Add(gidEvent)
						announcementBody := eventBody["announcement_body"].(map[string]any)
						switch announcementBody["gid"].(type) {
						case string:
							gidAnnouncement = announcementBody["gid"].(string)
						case float64:
							gidEvent = fmt.Sprintf("%f", announcementBody["gid"].(float64))
						default:
							log.ERROR.Printf("Gid for announcement is %s not string or float64", reflect.TypeOf(announcementBody["gid"]).String())
						}
						game.TotalUpvotes.Int32 += int32(announcementBody["voteupcount"].(float64))
						game.TotalDownvotes.Int32 += int32(announcementBody["votedowncount"].(float64))
						game.TotalComments.Int32 += int32(announcementBody["commentcount"].(float64))
						eventsProcessed++
					}
				}

				if eventsProcessed == 0 {
					return brk
				}
				return con
			}()

			if rt == con {
				continue
			}
			break
		}

		// Set the standardised URL for the game's website
		standardisedURL = browser.SteamAppPage.Fill(args...)
	default:
		panic(fmt.Errorf("scraping procedure for Storefront %s has not yet been implemented", sf.String()))
	}
	return
}

// ScrapeStorefrontsForGame will scrape all the storefront URLs found for a given game and collate more data on it that
// will be stored in a Game model instance.
func ScrapeStorefrontsForGame(storefrontsFound map[Storefront]mapset.Set[string]) *Game {
	game := (*Game)(nil)
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
				standardisedURL := storefront.ScrapeGame(url, game)
				// Finally, set the Storefront and Website fields
				game.Storefront = SteamStorefront
				game.Website = null.StringFrom(standardisedURL)
			}
		}
	}
	return game
}
