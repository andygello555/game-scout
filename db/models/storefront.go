package models

import (
	"database/sql/driver"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/anaskhan96/soup"
	"github.com/andygello555/game-scout/browser"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/andygello555/game-scout/steamcmd"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	"github.com/volatiletech/null/v9"
	"reflect"
	"regexp"
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

// TagConfig contains the configuration for the tags that are found in browser.SteamSpyAppDetails.
type TagConfig interface {
	TagDefaultValue() float64
	TagUpvotesThreshold() float64
	TagValues() map[string]float64
}

// StorefrontConfig contains the configuration for a specific Storefront.
type StorefrontConfig interface {
	StorefrontStorefront() Storefront
	StorefrontTags() TagConfig
}

// ScrapeConfig contains the configuration for the scrape.
type ScrapeConfig interface {
	ScrapeStorefronts() []StorefrontConfig
	ScrapeGetStorefront(storefront Storefront) StorefrontConfig
}

// ScrapeGame will fetch more info on the given game located at the given URL. The scrape procedure depends on what
// Storefront the URL is for. This also returns a standardised URL for the game's webpage.
func (sf Storefront) ScrapeGame(url string, game *Game, config ScrapeConfig) (standardisedURL string) {
	var err error
	switch sf {
	case UnknownStorefront:
		panic(fmt.Errorf("cannot scrape game on UnknownStorefront"))
	case SteamStorefront:
		args := browser.SteamAppPage.ExtractArgs(url)
		appID := args[0]
		const maxTries = 3
		const minDelay = time.Second * 2

		if err = myErrors.Retry(maxTries, minDelay, func(currentTry int, maxTries int, minDelay time.Duration, args ...any) (err error) {
			cmd := steamcmd.New(true)
			if err = cmd.Flow(
				steamcmd.NewCommandWithArgs(steamcmd.AppInfoPrint, appID),
				steamcmd.NewCommandWithArgs(steamcmd.Quit),
			); err != nil {
				return errors.Wrapf(err, "could not execute flow for app %d", appID)
			}

			var (
				ok           bool
				jsonBody     map[string]any
				appDetails   map[string]any
				appName      any
				associations map[string]any
			)

			if jsonBody, ok = cmd.ParsedOutputs[0].(map[string]any); !ok {
				return fmt.Errorf("cannot assert parsed output of app_info_print for %v to map", appID)
			}

			if appDetails, ok = jsonBody["common"].(map[string]any); !ok {
				return fmt.Errorf("cannot get \"common\" from parsed output for %v", appID)
			}

			if appName, ok = appDetails["name"]; !ok {
				return fmt.Errorf("cannot find \"name\" key in common details for %v", appID)
			}
			game.Name = null.StringFrom(appName.(string))
			log.INFO.Printf("Name of app %v is %s", appID, game.Name.String)

			if associations, ok = appDetails["associations"].(map[string]any); !ok {
				return fmt.Errorf("cannot find \"associations\" key or could not assert it to a map for %v", appID)
			}

			var developerName, publisherName strings.Builder
			for i, association := range associations {
				var associationMap map[string]any
				if associationMap, ok = association.(map[string]any); !ok {
					return fmt.Errorf("association %s in details for %v is not a map", i, appID)
				}

				switch associationMap["type"].(string) {
				case "developer":
					developerName.WriteString(associationMap["name"].(string) + ",")
				case "publisher":
					publisherName.WriteString(associationMap["name"].(string) + ",")
				default:
					break
				}
			}

			log.INFO.Printf("Publisher for %v: %s", appID, publisherName.String())
			log.INFO.Printf("Developer for %v: %s", appID, developerName.String())
			if developerName.String() != publisherName.String() {
				game.Publisher = null.StringFrom(strings.TrimRight(publisherName.String(), ","))
			}
			return
		}); err != nil {
			log.WARNING.Printf("Could not fetch info from SteamCMD for %d: %s", appID, err.Error())
		}

		// Fetch reviews to gather headline stats on the number of reviews
		if err = browser.SteamAppReviews.RetryJSON(maxTries, minDelay, func(jsonBody map[string]any) error {
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

		// Fetch all the community posts and aggregate the upvotes, downvotes, and comment totals
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

		// Use SteamSpy to find the accumulated TagScore for the game
		storefrontConfig := config.ScrapeGetStorefront(sf)
		if storefrontConfig != nil {
			tagConfig := storefrontConfig.StorefrontTags()
			if err = browser.SteamSpyAppDetails.RetryJSON(maxTries, minDelay, func(jsonBody map[string]any) (err error) {
				// We fall back to SteamSpy for fetching the game's name and publisher if we haven't got them yet
				if !game.Name.IsValid() && !game.Publisher.IsValid() {
					log.INFO.Printf(
						"We have not yet found the name and publisher for appID %d, falling back to SteamSpy",
						appID,
					)
					if name, ok := jsonBody["name"]; ok {
						var nameString string
						if nameString, ok = name.(string); ok {
							game.Name = null.StringFrom(nameString)
						}
					}

					publisher, publisherOk := jsonBody["publisher"]
					developer, developerOk := jsonBody["developer"]
					var (
						publisherName, developerName     string
						publisherNameOk, developerNameOk bool
					)
					log.INFO.Printf("AppID %d SteamSpy publisher: %v, ", appID, publisher)
					log.INFO.Printf("AppID %d SteamSpy developer: %v, ", appID, developer)
					switch {
					case publisherOk && developerOk, publisherOk && !developerOk:
						if publisherName, publisherNameOk = publisher.(string); !publisherNameOk {
							break
						}
						developerName, developerOk = "", true
						if developerOk {
							developerName, developerNameOk = developer.(string)
						}
						if (publisherNameOk && developerNameOk) && (publisherName != developerName) {
							game.Publisher = null.StringFrom(publisherName)
						}
					case !publisherOk && developerOk:
						// Developer is set but publisher isn't. So we assume that the game has no publisher
						fallthrough
					default:
						break
					}
				}

				if tagAny, ok := jsonBody["tags"]; ok {
					var tags map[string]any
					switch tagAny.(type) {
					case []any:
						if len(tagAny.([]any)) > 0 {
							return errors.New("\"tags\" should be an object but it's a non-empty array")
						}
						tags = make(map[string]any)
					case map[string]any:
						tags = tagAny.(map[string]any)
					default:
						return errors.Errorf("\"tags\" should be an object but it's a %s", reflect.TypeOf(tagAny).String())
					}

					if len(tags) > 0 {
						game.TagScore = null.Float64From(0.0)
					}
					for name, upvotes := range tags {
						// First we check if the upvotes are actually a float64
						var upvotesFloat float64
						if upvotesFloat, ok = upvotes.(float64); ok {
							upvotesFloat = upvotes.(float64)
						} else {
							log.WARNING.Printf(
								"For app %d: upvotes (%v) for tag \"%s\" is not a float, it is a %s",
								appID, upvotes, name, reflect.TypeOf(upvotes).String(),
							)
							continue
						}

						// Then we check if the upvotes exceed the upvotes threshold.
						if upvotesFloat < tagConfig.TagUpvotesThreshold() {
							log.WARNING.Printf(
								"For app %d: %f upvotes for \"%s\" do not exceed the threshold of %f",
								appID, upvotesFloat, name, tagConfig.TagUpvotesThreshold(),
							)
							continue
						}

						// Finally, we add the value of the tag to the tag score, using the default value if necessary
						var value float64
						if value, ok = tagConfig.TagValues()[name]; !ok {
							value = tagConfig.TagDefaultValue()
						}
						// Add the value of the tag multiplied by the number of upvotes the tag has
						log.INFO.Printf("For app %d: \"%s\" = %f", appID, name, value*upvotesFloat)
						game.TagScore.Float64 += value * upvotesFloat
					}

					// Then we take the average of the score
					if len(tags) > 0 && game.TagScore.Float64 > 0.0 {
						game.TagScore.Float64 = game.TagScore.Float64 / float64(len(tags))
					}
				}
				return
			}, appID); err != nil {
				log.WARNING.Printf(
					"Could not get SteamSpyAppDetails JSON for %s: %s",
					browser.SteamSpyAppDetails.Fill(appID), err.Error(),
				)
			}
		} else {
			log.WARNING.Printf("There is no storefront config for %s, so we are skipping tag finding", sf.String())
		}

		// Finally, we will fetch the game's app page so that we can see if they have included a link to the
		// dev's/game's twitter page. This is only done when there is a Developer filled for the Game
		// (i.e. the Game's Developer field is not nil).
		if game.Developer != nil {
			twitterUserURLPattern := regexp.MustCompile(`^https?://(?:www\.)?twitter\.com/(?:#!/)?@?([^/?#]*)(?:[?#].*)?$`)
			if err = browser.SteamAppPage.RetrySoup(maxTries, minDelay, func(doc *soup.Root) (err error) {
				links := doc.FindAll("a")
				usernames := mapset.NewSet[string]()
				for _, link := range links {
					attrs := link.Attrs()
					if href, ok := attrs["href"]; ok {
						if twitterUserURLPattern.MatchString(href) {
							subs := twitterUserURLPattern.FindStringSubmatch(href)
							if len(subs) > 0 {
								usernames.Add(subs[1])
							}
						}
					}
				}

				game.DeveloperVerified = usernames.Contains(game.Developer.Username)
				log.INFO.Printf(
					"Twitter usernames found for %d: %v. Contains \"%s\" = %t",
					appID, usernames, game.Developer.Username, game.DeveloperVerified,
				)
				return
			}, appID); err != nil {
				log.WARNING.Printf(
					"Could not get SteamAppPage Soup for %s: %s",
					browser.SteamAppPage.Fill(appID), err.Error(),
				)
			}
		} else {
			log.WARNING.Printf("Game's Developer field is null, skipping DeveloperVerified check...")
		}

		// Set the standardised URL for the game's website
		standardisedURL = browser.SteamAppPage.Fill(args...)
	default:
		panic(fmt.Errorf("scraping procedure for Storefront %s has not yet been implemented", sf.String()))
	}
	return
}

// ScrapeStorefrontsForGame will scrape all the storefront URLs found for a given game and collate more data on it that
// will be stored in the given Game model instance. The game parameter is a pointer to a pointer of Game so that we
// can set the Game to nil if there are no storefrontsFound for it.
func ScrapeStorefrontsForGame(game **Game, storefrontsFound map[Storefront]mapset.Set[string], config ScrapeConfig) {
	if len(storefrontsFound) > 0 {
		if game == nil {
			*game = &Game{}
		}
		(*game).Storefront = UnknownStorefront
		(*game).Name = null.StringFromPtr(nil)
		(*game).Website = null.StringFromPtr(nil)
		(*game).Publisher = null.StringFromPtr(nil)
		(*game).TotalReviews = null.Int32FromPtr(nil)
		(*game).PositiveReviews = null.Int32FromPtr(nil)
		(*game).NegativeReviews = null.Int32FromPtr(nil)
		(*game).ReviewScore = null.Float64FromPtr(nil)
		(*game).TotalUpvotes = null.Int32FromPtr(nil)
		(*game).TotalDownvotes = null.Int32FromPtr(nil)
		(*game).TotalComments = null.Int32FromPtr(nil)
		(*game).TagScore = null.Float64FromPtr(nil)
		for _, storefront := range UnknownStorefront.Storefronts() {
			if _, ok := storefrontsFound[storefront]; ok {
				// Pop a random URL from the set of URLs for this storefront
				url, _ := storefrontsFound[SteamStorefront].Pop()
				// Do some scraping to find more details for the game
				standardisedURL := storefront.ScrapeGame(url, *game, config)
				// Finally, set the Storefront and Website fields
				(*game).Storefront = SteamStorefront
				(*game).Website = null.StringFrom(standardisedURL)
			}
		}
	} else {
		*game = nil
	}
}
