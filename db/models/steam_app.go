package models

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/anaskhan96/soup"
	"github.com/andygello555/game-scout/browser"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/andygello555/game-scout/steamcmd"
	myTwitter "github.com/andygello555/game-scout/twitter"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"github.com/volatiletech/null/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SteamApp represents an app on Steam that has been consumed from the ScoutWebPipes co-process. It can be
// created/updated in the websocket client that reads info pushed from the ScoutWebPipes process.
type SteamApp struct {
	// ID directly matches a Steam app's appID.
	ID uint64
	// Name is the current name of the SteamApp.
	Name string
	// CreatedAt is when this SteamApp was first created. SteamApp's that were CreatedAt closer to the current update
	// time are desired more than SteamApp's that were CreatedAt further in the past.
	CreatedAt time.Time
	// Updates is the number of times this SteamApp has been updated in the DB. This is not the number of changelist
	// that have occurred for this title.
	Updates uint64
	// DeveloperID is the foreign key to a Developer on Twitter. This can be nil/null, in case there is no socials
	// linked on the SteamApp's store page.
	DeveloperID *string
	Developer   *Developer `gorm:"constraint:OnDelete:CASCADE;"`
	// ReleaseDate is when this game was/is going to be released on SteamStorefront.
	ReleaseDate time.Time
	// Publisher is the publisher for this game. If this cannot be found it is set to nil. If this is set then it
	// negatively contributes to the SteamApp's WeightedScore.
	Publisher null.String
	// TotalReviews for this SteamApp.
	TotalReviews int32
	// PositiveReviews for this SteamApp.
	PositiveReviews int32
	// NegativeReviews for this SteamApp.
	NegativeReviews int32
	// ReviewScore for this game (PositiveReviews / TotalReviews). This is a computed field, no need to set it before
	// saving.
	ReviewScore float64
	// TotalUpvotes for this SteamApp on news posts from the developer on Steam community.
	TotalUpvotes int32
	// TotalDownvotes for this SteamApp on news posts from the developer on Steam community.
	TotalDownvotes int32
	// TotalComments for this SteamApp on news posts from the developer on Steam community.
	TotalComments int32
	// TagScore is average value of each community voted tag for the SteamApp. Each tag's value is calculated by
	// multiplying the number of upvotes for that tag by the default value/override value for that tag. See TagConfig for
	// more info.
	TagScore float64
	// AssetModifiedTime is the last time an asset was modified on this SteamApp's store page.
	AssetModifiedTime time.Time
	// LastChangelistID is the ID of the last changelist that was read for this title.
	LastChangelistID uint64
	// WeightedScore is a weighted average comprised of the values taken from CreatedAt, Updates, ReleaseDate,
	// Publisher, TotalReviews, ReviewScore, TotalUpvotes, TotalDownvotes, TotalComments, TagScore, and the
	// AssetModifiedTime for the SteamApp. If SteamApp.CheckCalculateWeightedScore is false then this will be nil. This
	// is a computed field, no need to set it before saving.
	WeightedScore null.Float64
}

// steamAppWeight represents a weight for a steamAppWeightedField. If the steamAppWeight is negative then this means to
// take the inverse of the value first, then multiply it by the math.Abs(steamAppWeight).
type steamAppWeight float64

const (
	SteamAppPublisherWeight         steamAppWeight = 0.55
	SteamAppTotalReviewsWeight      steamAppWeight = -0.75
	SteamAppReviewScoreWeight       steamAppWeight = 0.65
	SteamAppTotalUpvotesWeight      steamAppWeight = 0.45
	SteamAppTotalDownvotesWeight    steamAppWeight = 0.25
	SteamAppTotalCommentsWeight     steamAppWeight = 0.35
	SteamAppTagScoreWeight          steamAppWeight = 0.25
	SteamAppUpdatesWeight           steamAppWeight = -0.55
	SteamAppAssetModifiedTimeWeight steamAppWeight = -0.2
	SteamAppCreatedAtWeight         steamAppWeight = -0.8
	SteamAppReleaseDateWeight       steamAppWeight = 0.6
)

// steamAppWeightedField represents a field that can have a weighting calculation applied to it in SteamApp.
type steamAppWeightedField string

const (
	SteamAppPublisher         steamAppWeightedField = "Publisher"
	SteamAppTotalReviews      steamAppWeightedField = "TotalReviews"
	SteamAppReviewScore       steamAppWeightedField = "ReviewScore"
	SteamAppTotalUpvotes      steamAppWeightedField = "TotalUpvotes"
	SteamAppTotalDownvotes    steamAppWeightedField = "TotalDownvotes"
	SteamAppTotalComments     steamAppWeightedField = "TotalComments"
	SteamAppTagScore          steamAppWeightedField = "TagScore"
	SteamAppUpdates           steamAppWeightedField = "Updates"
	SteamAppAssetModifiedTime steamAppWeightedField = "AssetModifiedTime"
	SteamAppCreatedAt         steamAppWeightedField = "CreatedAt"
	SteamAppReleaseDate       steamAppWeightedField = "ReleaseDate"
)

// String returns the string value of the gameWeightedField.
func (sf steamAppWeightedField) String() string { return string(sf) }

// Weight returns the steamAppWeight for a steamAppWeightedField, as well as whether the value should have its inverse
// taken first.
func (sf steamAppWeightedField) Weight() (w float64, inverse bool) {
	switch sf {
	case SteamAppPublisher:
		w = float64(SteamAppPublisherWeight)
	case SteamAppTotalReviews:
		w = float64(SteamAppTotalReviewsWeight)
	case SteamAppReviewScore:
		w = float64(SteamAppReviewScoreWeight)
	case SteamAppTotalUpvotes:
		w = float64(SteamAppTotalUpvotesWeight)
	case SteamAppTotalDownvotes:
		w = float64(SteamAppTotalDownvotesWeight)
	case SteamAppTotalComments:
		w = float64(SteamAppTotalCommentsWeight)
	case SteamAppTagScore:
		w = float64(SteamAppTagScoreWeight)
	case SteamAppUpdates:
		w = float64(SteamAppUpdatesWeight)
	case SteamAppAssetModifiedTime:
		w = float64(SteamAppAssetModifiedTimeWeight)
	case SteamAppCreatedAt:
		w = float64(SteamAppCreatedAtWeight)
	case SteamAppReleaseDate:
		w = float64(SteamAppReleaseDateWeight)
	default:
		panic(fmt.Errorf("\"%s\" is not a steamAppWeightedField", sf))
	}
	inverse = w < 0.0
	w = math.Abs(w)
	return
}

// GetValueFromWeightedModel uses reflection to get the value of the steamAppWeightedField from the given SteamApp, and
// will return a list of floats for use in the calculation of the SteamApp.WeightedScore.
func (sf steamAppWeightedField) GetValueFromWeightedModel(model WeightedModel) []float64 {
	r := reflect.ValueOf(model)
	f := reflect.Indirect(r).FieldByName(sf.String())
	switch sf {
	case SteamAppPublisher:
		nullString := f.Interface().(null.String)
		val := -7000.0
		if !nullString.IsValid() {
			val = 4000.0
		}
		return []float64{val}
	case SteamAppTotalReviews:
		totalReviews := f.Int()
		if totalReviews > 5000 {
			totalReviews = 5000
		}
		return []float64{float64(totalReviews) / 1000000.0}
	case SteamAppReviewScore:
		return []float64{f.Float() * 1500.0}
	case SteamAppTotalUpvotes, SteamAppTotalDownvotes, SteamAppTotalComments:
		return []float64{float64(f.Int() * 2)}
	case SteamAppTagScore:
		return []float64{f.Float()}
	case SteamAppUpdates:
		return []float64{float64(f.Uint()) / 1500.0}
	case SteamAppAssetModifiedTime, SteamAppCreatedAt:
		// The value for a time is calculated by subtracting the time value from the time now, then clamping
		// this duration to be +5 months, then finally divide the number of hours of the duration by 50000 as these
		// weights are inverse.
		// Note: these times will always be before or equal to the current time, so we don't have to deal with
		//       negative clamping.
		timeDiff := time.Now().UTC().Sub(f.Interface().(time.Time))
		if timeDiff > time.Hour*24*30*5 {
			timeDiff = time.Hour * 24 * 30 * 5
		}
		return []float64{timeDiff.Hours() / 50000.0}
	case SteamAppReleaseDate:
		// The value for release date is calculated by subtracting the time now from the release date, then finding the
		// hours for that duration. We also subtract 1 month from the duration, so we still look positively on games
		// that have been released one month before today. Finally, we clamp the duration to be between +/- 5
		// months.
		timeDiff := f.Interface().(time.Time).Sub(time.Now().UTC()) - time.Hour*24*30
		if timeDiff.Abs() > time.Hour*24*30*5 {
			timeDiff = map[bool]time.Duration{true: -1, false: 1}[timeDiff < 0] * time.Hour * 24 * 30 * 5
		}
		return []float64{timeDiff.Hours()}
	default:
		panic(fmt.Errorf("steamAppWeightedField %s is not recognized, and cannot be converted to []float64", sf))
	}
}

func (sf steamAppWeightedField) Fields() []WeightedField {
	return []WeightedField{
		SteamAppPublisher,
		SteamAppTotalReviews,
		SteamAppReviewScore,
		SteamAppTotalUpvotes,
		SteamAppTotalDownvotes,
		SteamAppTotalComments,
		SteamAppTagScore,
		SteamAppUpdates,
		SteamAppAssetModifiedTime,
		SteamAppCreatedAt,
	}
}

func (app *SteamApp) Empty() any {
	return &SteamApp{}
}

// UpdateComputedFields will update the fields in a SteamApp that are computed.
func (app *SteamApp) UpdateComputedFields(tx *gorm.DB) (err error) {
	// Calculate the WeightedScore
	app.WeightedScore = null.Float64FromPtr(nil)
	if app.CheckCalculateWeightedScore() {
		app.WeightedScore = null.Float64From(CalculateWeightedScore(app, SteamAppUpdates))
	}
	return
}

// CheckCalculateWeightedScore will only return true when the number of Updates exceeds 0 and the game has no Publisher.
// This is to stop the weights of games that have not yet been scraped from being calculated.
func (app *SteamApp) CheckCalculateWeightedScore() bool {
	return app.Updates > 0 && !app.Publisher.IsValid()
}

func (app *SteamApp) BeforeCreate(tx *gorm.DB) (err error) {
	if err = app.UpdateComputedFields(tx); err != nil {
		err = errors.Wrapf(err, "could not update computed fields for SteamApp %d", app.ID)
	}
	return
}

func (app *SteamApp) BeforeUpdate(tx *gorm.DB) (err error) {
	// Increment the updates counter. Updates is not a computed field so should not be included in UpdateComputedField
	app.Updates++
	if err = app.UpdateComputedFields(tx); err != nil {
		err = errors.Wrapf(err, "could not update computed fields for SteamApp %d", app.ID)
	}
	return
}

// OnConflict returns the clause.OnConflict that should be checked in an upsert clause.
func (app *SteamApp) OnConflict() clause.OnConflict {
	return clause.OnConflict{Columns: []clause.Column{{Name: "id"}}}
}

// OnCreateOmit returns the fields that should be omitted when creating a SteamApp.
func (app *SteamApp) OnCreateOmit() []string {
	return []string{"Developer"}
}

func (app *SteamApp) Update(db *gorm.DB, config ScrapeConfig) error {
	ScrapeStorefrontForGameModel[uint64](app.ID, app.Wrapper().StorefrontScraper(SteamStorefront), config)
	return db.Omit(app.OnCreateOmit()...).Save(app).Error
}

func (app *SteamApp) Wrapper() GameModelWrapper[uint64, GameModel[uint64]] {
	return &SteamAppWrapper{SteamApp: app}
}
func (app *SteamApp) GetID() uint64 { return app.ID }

type SteamAppWrapper struct{ SteamApp *SteamApp }

func (s *SteamAppWrapper) Default() {
	if s.SteamApp == nil {
		s.SteamApp = &SteamApp{}
	}
	s.SteamApp.Publisher = null.StringFromPtr(nil)
}

func (s *SteamAppWrapper) Nil()                   { s.SteamApp = nil }
func (s *SteamAppWrapper) Get() GameModel[uint64] { return s.SteamApp }

func (s *SteamAppWrapper) StorefrontScraper(storefront Storefront) GameModelStorefrontScraper[uint64] {
	switch storefront {
	case SteamStorefront:
		return &SteamAppSteamStorefront{SteamApp: s.SteamApp}
	default:
		return UnscrapableError[uint64](storefront)
	}
}

type SteamAppSteamStorefront SteamAppWrapper

func (s *SteamAppSteamStorefront) Args(id uint64) []any      { return []any{id} }
func (s *SteamAppSteamStorefront) GetStorefront() Storefront { return SteamStorefront }

func (s *SteamAppSteamStorefront) ScrapeInfo(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	appID := args[0]
	return myErrors.Retry(maxTries, minDelay, func(currentTry int, maxTries int, minDelay time.Duration, args ...any) (err error) {
		log.INFO.Printf("SteamCMD \"app_info_print %d\" is on try %d/%d", appID, currentTry, maxTries)
		cmd := steamcmd.New(true)
		if err = cmd.Flow(
			steamcmd.NewCommandWithArgs(steamcmd.AppInfoPrint, appID),
			steamcmd.NewCommandWithArgs(steamcmd.Quit),
		); err != nil {
			return errors.Wrapf(err, "could not execute flow for app %d", appID)
		}

		var (
			ok                bool
			jsonBody          map[string]any
			appDetails        map[string]any
			appName           any
			appAssetMTime     string
			appAssetMTimeInt  int64
			appReleaseDate    string
			appReleaseDateInt int64
			associations      map[string]any
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
		s.SteamApp.Name = appName.(string)
		log.INFO.Printf("Name of app %v is %s", appID, s.SteamApp.Name)

		// We then get the last time the assets were modified on the store page so that we can set AssetModifiedTime
		if appAssetMTime, ok = appDetails["store_asset_mtime"].(string); !ok {
			log.WARNING.Printf("Cannot find \"store_asset_mtime\" key in common details for %v", appID)
		} else {
			if appAssetMTimeInt, err = strconv.ParseInt(appAssetMTime, 10, 64); err != nil {
				return errors.Wrapf(
					err,
					"could not convert store_asset_mtime timestamp (\"%v\") to int for %v",
					appAssetMTime, appID,
				)
			}
			s.SteamApp.AssetModifiedTime = time.Unix(appAssetMTimeInt, 0)
			log.INFO.Printf("App %v had its assets last modified on %s", appID, s.SteamApp.AssetModifiedTime.String())
		}

		// steam_release_date is a timestamp as a string. This is our primary way of finding the release date of the
		// SteamApp. However, it sometimes does not appear for unreleased games. In cases where it doesn't appear we will
		// skip over it rather than retry the flow.
		if appReleaseDate, ok = appDetails["steam_release_date"].(string); !ok {
			log.WARNING.Printf(
				"Cannot find \"steam_release_date\" key in common details for %v. Unreleased? Further info to come "+
					"in ScrapeExtra",
				appID,
			)
		} else {
			// Parse the release date timestamp to an int and then convert it to a time using time.Unix
			if appReleaseDateInt, err = strconv.ParseInt(appReleaseDate, 10, 64); err != nil {
				return errors.Wrapf(err,
					"could not convert release date timestamp (\"%v\") to int for %v",
					appReleaseDate, appID,
				)
			}
			s.SteamApp.ReleaseDate = time.Unix(appReleaseDateInt, 0)
			log.INFO.Printf("App %v is released/is going to be released on %s", appID, s.SteamApp.ReleaseDate.String())
		}

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
			s.SteamApp.Publisher = null.StringFrom(strings.TrimRight(publisherName.String(), ","))
		}
		return
	})
}

func (s *SteamAppSteamStorefront) ScrapeReviews(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	appID := args[0]
	// Fetch reviews to gather headline stats on the number of reviews
	return browser.SteamAppReviews.RetryJSON(maxTries, minDelay, func(jsonBody map[string]any) error {
		querySummary, ok := jsonBody["query_summary"].(map[string]any)
		if ok {
			s.SteamApp.TotalReviews = int32(querySummary["total_reviews"].(float64))
			s.SteamApp.PositiveReviews = int32(querySummary["total_positive"].(float64))
			s.SteamApp.NegativeReviews = int32(querySummary["total_negative"].(float64))
			log.INFO.Printf(
				"For game %d: total_reviews = %d, positive_reviews = %d, negative_reviews = %d",
				appID, s.SteamApp.TotalReviews, s.SteamApp.PositiveReviews, s.SteamApp.NegativeReviews,
			)
		} else {
			return fmt.Errorf("could not find query_summary key in SteamAppReviews response JSON")
		}
		return nil
	}, appID, "*", "all", 20, "all", "all", -1, -1, "all")
}

func (s *SteamAppSteamStorefront) ScrapeCommunity(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) (err error) {
	appID := args[0]
	// Fetch all the community posts and aggregate the upvotes, downvotes, and comment totals
	const batchSize = 50
	gidEvent := "0"
	gidAnnouncement := "0"
	gids := mapset.NewThreadUnsafeSet[string]()
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
				log.WARNING.Printf("Could not get SteamCommunityPosts JSON for %d: %s. Tries left: %d", appID, err.Error(), tries)
				if tries > 0 {
					tries--
					time.Sleep(minDelay * time.Duration(maxTries+1-tries))
					return con
				}
				return brk
			}
			eventsProcessed := 0

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
					s.SteamApp.TotalUpvotes += int32(announcementBody["voteupcount"].(float64))
					s.SteamApp.TotalDownvotes += int32(announcementBody["votedowncount"].(float64))
					s.SteamApp.TotalComments += int32(announcementBody["commentcount"].(float64))
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
	return
}

func (s *SteamAppSteamStorefront) ScrapeTags(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	appID := args[0]
	// Use SteamSpy to find the accumulated TagScore for the game
	storefrontConfig := config.ScrapeGetStorefront(SteamStorefront)
	tagConfig := storefrontConfig.StorefrontTags()
	return browser.SteamSpyAppDetails.RetryJSON(maxTries, minDelay, func(jsonBody map[string]any) (err error) {
		// We fall back to SteamSpy for fetching the game's name and publisher if we haven't got them yet
		if s.SteamApp.Name == "" || !s.SteamApp.Publisher.IsValid() {
			log.INFO.Printf(
				"We have not yet found the name and publisher for appID %d, falling back to SteamSpy",
				appID,
			)
			if name, ok := jsonBody["name"]; ok {
				var nameString string
				if nameString, ok = name.(string); ok {
					s.SteamApp.Name = nameString
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
					s.SteamApp.Publisher = null.StringFrom(publisherName)
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
				s.SteamApp.TagScore += value * upvotesFloat
			}

			// Then we take the average of the score
			if len(tags) > 0 && s.SteamApp.TagScore > 0.0 {
				s.SteamApp.TagScore = s.SteamApp.TagScore / float64(len(tags))
			}
		}
		return
	}, appID)
}

func (s *SteamAppSteamStorefront) ScrapeExtra(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) (err error) {
	appID := args[0]
	twitterUserURLPattern := regexp.MustCompile(`^https?://(?:www\.)?twitter\.com/(?:#!/)?@?([^/?#]*)(?:[?#].*)?$`)
	// Make a request to the SteamAppPage for the appID so that we can find any Twitter usernames
	return browser.SteamAppPage.RetrySoup(maxTries, minDelay, func(doc *soup.Root) (err error) {
		links := doc.FindAll("a")
		usernames := mapset.NewThreadUnsafeSet[string]()
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

		// Remove Steam's Twitter account
		usernames.Remove("steam")
		username, _ := usernames.Pop()
		log.INFO.Printf("Scraping Twitter username %s found for %d", username, appID)

		// Fetch details on all the other Twitter accounts
		var response myTwitter.BindingResult
		opts := twitter.UserLookupOpts{
			UserFields: []twitter.UserField{
				twitter.UserFieldDescription,
				twitter.UserFieldEntities,
				twitter.UserFieldPublicMetrics,
				twitter.UserFieldVerified,
				twitter.UserFieldPinnedTweetID,
				twitter.UserFieldCreatedAt,
			},
		}
		if response, err = myTwitter.Client.ExecuteBinding(myTwitter.UserNameRetrieve, nil, []string{username}, opts); err != nil {
			return errors.Wrapf(err, "could not fetch details for Twitter social %s for %d", username, appID)
		}

		// Create the Developer instance. Because we don't have access to the DB from here we will just set the
		// SteamApp.Developer so that it can be created later down the line.
		developer := Developer{}
		for _, user := range response.Raw().(*twitter.UserRaw).UserDictionaries() {
			developer.ID = user.User.ID
			developer.Name = user.User.Name
			developer.Username = user.User.UserName
			developer.Description = user.User.Description
			developer.ProfileCreated, _ = time.Parse(myTwitter.CreatedAtFormat, user.User.CreatedAt)
			developer.PublicMetrics = user.User.PublicMetrics
		}
		s.SteamApp.DeveloperID = &developer.ID
		s.SteamApp.Developer = &developer

		// This is a fallback for finding the release date for games that are unreleased.
		if s.SteamApp.ReleaseDate.IsZero() {
			if date := doc.Find("div", "class", "date"); date.Error != nil {
				log.WARNING.Printf("Could not find release date on store page for %d: %v", appID, date.Error.Error())
			} else {
				releaseDateString := strings.TrimSpace(date.Text())
				var releaseDate time.Time
				if releaseDate, err = steamcmd.ParseSteamDate(releaseDateString); err != nil {
					releaseDate = time.Now().UTC().Add(time.Hour * 24 * 30 * 5)
					log.WARNING.Printf(
						"Could not parse release date \"%s\" for %d, assuming that game is coming soon: %s",
						releaseDateString, appID, releaseDate.String(),
					)
				}
				s.SteamApp.ReleaseDate = releaseDate
				log.INFO.Printf("ReleaseDate found for %d: %s", appID, s.SteamApp.ReleaseDate.String())
			}
		}
		return
	}, appID)
}

func (s *SteamAppSteamStorefront) AfterScrape(args ...any) {}

// CreateInitialSteamApps will use the browser.SteamGetAppList to create initial SteamApp for each app on Steam if the
// number of SteamApp is 0. This should technically only ever run once on the creation of the SteamApp table.
func CreateInitialSteamApps(db *gorm.DB) (err error) {
	var count int64
	if err = db.Model(&SteamApp{}).Count(&count).Error; err != nil {
		err = errors.Wrap(err, "could not find the number of SteamApps in the DB")
		return
	}

	if count == 0 {
		apps := make(map[uint64]*SteamApp)
		if err = browser.SteamGetAppList.RetryJSON(3, time.Second*20, func(jsonBody map[string]any) (err error) {
			var (
				appObjs []any
				ok      bool
			)
			if appObjs, ok = jsonBody["applist"].(map[string]any)["apps"].([]any); !ok {
				err = errors.Wrap(err, "applist.apps does not exist in GetAppList response or it is not a []any")
				return
			}

			if len(appObjs) == 0 {
				return fmt.Errorf("GetAppList response contains no apps")
			}

			for _, appObj := range appObjs {
				app := &SteamApp{Publisher: null.StringFromPtr(nil), Updates: 0}
				var appID float64
				if appID, ok = appObj.(map[string]any)["appid"].(float64); ok {
					app.ID = uint64(appID)
					if app.Name, ok = appObj.(map[string]any)["name"].(string); ok {
						apps[app.ID] = app
					}
				}
			}
			return
		}); err != nil {
			err = errors.Wrap(err, "could not create initial SteamApps because we couldn't fetch the response to GetAppList")
			return
		}

		if len(apps) == 0 {
			return fmt.Errorf("SteamAppList has found no SteamApps")
		}

		steamApps := make([]*SteamApp, len(apps))
		i := 0
		for _, steamApp := range apps {
			steamApps[i] = steamApp
			i++
		}
		if err = db.Create(&steamApps).Error; err != nil {
			err = errors.Wrapf(err, "could not create %d initial SteamApps", len(steamApps))
		}
	} else {
		log.WARNING.Printf("There are already %d SteamApps in the DB, skipping initial creation...", count)
	}
	return
}
