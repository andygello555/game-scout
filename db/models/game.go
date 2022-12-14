package models

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/anaskhan96/soup"
	"github.com/andygello555/game-scout/browser"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/andygello555/game-scout/steamcmd"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/google/uuid"
	"github.com/lib/pq"
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

// Game represents a game (supposedly) being developed by a Developer. The Website of which can be on one of many
// Storefront.
type Game struct {
	// ID is an uuid.UUID that is automatically generated by postgres.
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4()"`
	// Name is the name of the game. If this cannot be found it is set to nil.
	Name null.String
	// Storefront is where this game is sold. If this cannot be found it is set to nil.
	Storefront Storefront `gorm:"type:storefront_type"`
	// Website is the URL for this game's website. Usually a Steam store page.
	Website null.String `gorm:"index:idx_website,where:website is not null,unique"`
	// Developers are the Twitter usernames that could be developers for this Game.
	Developers pq.StringArray `gorm:"type:varchar(15)[];default:'{}'"`
	// VerifiedDeveloperUsernames are the Twitter usernames that can be found somewhere on the Game's Website.
	VerifiedDeveloperUsernames pq.StringArray `gorm:"type:varchar(15)[];default:'{}'"`
	// Updates is the number of times this app has been updated. 0 means that the Game has just been created. Please
	// don't set this yourself when creating a Game.
	Updates uint64
	// ReleaseDate is when the game was/is going to be released. If this cannot be found, then it is set to nil.
	ReleaseDate null.Time
	// Publisher is the publisher for this game. Usually found via the Steam store API. If this cannot be found it is
	// set to nil. If this is set then it negatively contributes to the Game's WeightedScore.
	Publisher null.String
	// TotalReviews for this game. Only set when Storefront is SteamStorefront. If this cannot be found it is set to
	// nil.
	TotalReviews null.Int32
	// PositiveReviews for this game. Only set when Storefront is SteamStorefront. If this cannot be found it is set to
	// nil.
	PositiveReviews null.Int32
	// NegativeReviews for this game. Only set when Storefront is SteamStorefront. If this cannot be found it is set to
	// nil.
	NegativeReviews null.Int32
	// ReviewScore for this game (PositiveReviews / TotalReviews). Only set when Storefront is SteamStorefront,
	// PositiveReviews is set, and NegativeReviews is set. This is a computed field, no need to set it before saving.
	ReviewScore null.Float64
	// TotalUpvotes for this game. Only set when Storefront is SteamStorefront.
	TotalUpvotes null.Int32
	// TotalDownvotes for this game. Only set when Storefront is SteamStorefront.
	TotalDownvotes null.Int32
	// TotalComments for this game. Only set when Storefront is SteamStorefront or ItchIOStorefront.
	TotalComments null.Int32
	// TagScore is average value of each tag. Each tag's value is calculated by multiplying the number of upvotes for
	// that tag by the default value/override value for that tag. See TagConfig for more info.
	TagScore null.Float64
	// WeightedScore is a weighted average comprised of the values taken from Publisher, TotalReviews, ReviewScore,
	// TotalUpvotes, TotalDownvotes, TotalComments, TagScore, and Updates for this game. If
	// Game.CheckCalculateWeightedScore is false then this will be nil. This is a computed field, no need to set it
	// before saving.
	WeightedScore null.Float64
}

// gameWeight represents a weight for a gameWeightedField. If the gameWeight is negative then this means to take the
// inverse of the value first, then multiply it by the math.Abs(gameWeight).
type gameWeight float64

const (
	GamePublisherWeight      gameWeight = 0.55
	GameTotalReviewsWeight   gameWeight = 0.75
	GameReviewScoreWeight    gameWeight = 0.65
	GameTotalUpvotesWeight   gameWeight = 0.45
	GameTotalDownvotesWeight gameWeight = 0.25
	GameTotalCommentsWeight  gameWeight = 0.35
	GameTagScoreWeight       gameWeight = 0.25
	GameUpdatesWeight        gameWeight = -0.15
	GameReleaseDateWeight    gameWeight = 0.7
)

// gameWeightedField represents a field that can have a weighting calculation applied to it in Game.
type gameWeightedField string

const (
	GamePublisher      gameWeightedField = "Publisher"
	GameTotalReviews   gameWeightedField = "TotalReviews"
	GameReviewScore    gameWeightedField = "ReviewScore"
	GameTotalUpvotes   gameWeightedField = "TotalUpvotes"
	GameTotalDownvotes gameWeightedField = "TotalDownvotes"
	GameTotalComments  gameWeightedField = "TotalComments"
	GameTagScore       gameWeightedField = "TagScore"
	GameUpdates        gameWeightedField = "Updates"
	GameReleaseDate    gameWeightedField = "ReleaseDate"
)

// String returns the string value of the gameWeightedField.
func (gf gameWeightedField) String() string { return string(gf) }

// Weight returns the gameWeight for a gameWeightedField, as well as whether the value should have its inverse taken
// first.
func (gf gameWeightedField) Weight() (w float64, inverse bool) {
	switch gf {
	case GamePublisher:
		w = float64(GamePublisherWeight)
	case GameTotalReviews:
		w = float64(GameTotalReviewsWeight)
	case GameReviewScore:
		w = float64(GameReviewScoreWeight)
	case GameTotalUpvotes:
		w = float64(GameTotalUpvotesWeight)
	case GameTotalDownvotes:
		w = float64(GameTotalDownvotesWeight)
	case GameTotalComments:
		w = float64(GameTotalCommentsWeight)
	case GameTagScore:
		w = float64(GameTagScoreWeight)
	case GameUpdates:
		w = float64(GameUpdatesWeight)
	case GameReleaseDate:
		w = float64(GameReleaseDateWeight)
	default:
		panic(fmt.Errorf("\"%s\" is not a gameWeightedField", gf))
	}
	inverse = w < 0.0
	w = math.Abs(w)
	return
}

// GetValueFromWeightedModel uses reflection to get the value of the gameWeightedField from the given Game, and will
// return a list of floats for use in the calculation of the Game.WeightedScore.
func (gf gameWeightedField) GetValueFromWeightedModel(model WeightedModel) []float64 {
	r := reflect.ValueOf(model)
	f := reflect.Indirect(r).FieldByName(gf.String())
	switch gf {
	case GamePublisher:
		nullString := f.Interface().(null.String)
		val := -50000.0
		if !nullString.IsValid() {
			val = 4000.0
		}
		return []float64{val}
	case GameReviewScore:
		nullFloat64 := f.Interface().(null.Float64)
		var val float64
		if nullFloat64.IsValid() {
			val = (*nullFloat64.Ptr()) * 1500.0
		}
		return []float64{val}
	case GameTotalReviews:
		nullInt32 := f.Interface().(null.Int32)
		var val float64
		if nullInt32.IsValid() {
			valInt := (*nullInt32.Ptr()) + 1
			if valInt > 5000 {
				valInt = 5000
			}
			val = ScaleRange(float64(valInt), 1.0, 5000.0, 1000000.0, -1000000.0)
		}
		return []float64{val}
	case GameTotalUpvotes, GameTotalDownvotes, GameTotalComments:
		nullInt32 := f.Interface().(null.Int32)
		var val float64
		if nullInt32.IsValid() {
			val = float64(*nullInt32.Ptr()) * 2
			if val > 5000 {
				val = 5000
			}
			val = ScaleRange(val*2, 0, 10000, -1000, 10000)
		}
		return []float64{val}
	case GameTagScore:
		nullInt64 := f.Interface().(null.Float64)
		var val float64
		if nullInt64.IsValid() {
			val = *nullInt64.Ptr()
		}
		return []float64{val}
	case GameUpdates:
		return []float64{float64(f.Uint()+1) / 1500.0}
	//case GameDeveloperVerified:
	//	val := -50000.0
	//	if f.Bool() {
	//		val = 4000.0
	//	}
	//	return []float64{val}
	case GameReleaseDate:
		nullTime := f.Interface().(null.Time)
		var val float64
		if nullTime.IsValid() && !nullTime.Time.IsZero() {
			// The value for release date is calculated by subtracting the time now from the release date, then finding the
			// hours for that duration. We also subtract 1 month from the duration, so we still look positively on games
			// that have been released one month before today. Finally, we clamp the duration to be between +/- 5
			// months.
			timeDiff := nullTime.Time.Sub(time.Now().UTC()) - time.Hour*24*30
			if timeDiff.Abs() > time.Hour*24*30*5 {
				timeDiff = map[bool]time.Duration{true: -1, false: 1}[timeDiff < 0] * time.Hour * 24 * 30 * 5
			}
			val = timeDiff.Hours()
		}
		return []float64{val}
	default:
		panic(fmt.Errorf("gameWeightedField %s is not recognized, and cannot be converted to []float64", gf))
	}
}

func (gf gameWeightedField) Fields() []WeightedField {
	return []WeightedField{
		GamePublisher,
		GameTotalReviews,
		GameReviewScore,
		GameTotalUpvotes,
		GameTotalDownvotes,
		GameTotalComments,
		GameTagScore,
		GameUpdates,
		GameReleaseDate,
	}
}

// CheckCalculateWeightedScore checks if we can calculate the WeightedScore for this Game. This is dependent on the
// Website field being set and the Storefront not being UnknownStorefront.
func (g *Game) CheckCalculateWeightedScore() bool {
	return g.Website.IsValid() && g.Storefront != UnknownStorefront
}

// checkCalculateReviewScore checks if we can calculate the GameReviewScore for this Game. This is dependent on the
// GameTotalReviews and PositiveReviews field.
func (g *Game) checkCalculateReviewScore() bool {
	return g.TotalReviews.IsValid() && g.PositiveReviews.IsValid()
}

// UpdateComputedFields will update the fields in Game that are computed.
func (g *Game) UpdateComputedFields(tx *gorm.DB) (err error) {
	// First we set the ReviewScore
	g.ReviewScore = null.Float64FromPtr(nil)
	if g.checkCalculateReviewScore() {
		if *g.TotalReviews.Ptr() == 0 {
			g.ReviewScore = null.Float64From(1.0)
		} else {
			g.ReviewScore = null.Float64From(float64(g.PositiveReviews.Int32) / float64(g.TotalReviews.Int32))
		}
	}

	// Then we set the WeightedScore
	g.WeightedScore = null.Float64FromPtr(nil)
	if g.CheckCalculateWeightedScore() {
		g.WeightedScore = null.Float64From(CalculateWeightedScore(g, GamePublisher))
	}
	return
}

func (g *Game) Empty() any {
	return &Game{}
}

func (g *Game) Order() string {
	return "id"
}

func (g *Game) BeforeCreate(tx *gorm.DB) (err error) {
	if err = g.UpdateComputedFields(tx); err != nil {
		err = errors.Wrapf(err, "could not update computed fields for Game %s", g.ID.String())
	}
	return
}

func (g *Game) BeforeUpdate(tx *gorm.DB) (err error) {
	// Increment the updates counter. Updates is not a computed field so should not be included in UpdateComputedField
	g.Updates++
	if err = g.UpdateComputedFields(tx); err != nil {
		err = errors.Wrapf(err, "could not update computed fields for Game %s", g.ID.String())
	}
	return
}

// OnConflict returns the clause.OnConflict that should be checked in an upsert clause.
func (g *Game) OnConflict() clause.OnConflict {
	// For the developers column we will perform a union on the existing Game row and this Game so that we don't
	// overwrite any previous updates.
	// Note: we do not have to do this with verified_developer_usernames as we always take the newest update to be gospel.
	doUpdates := clause.Assignments(map[string]interface{}{
		"developers": gorm.Expr("ARRAY(SELECT DISTINCT UNNEST(\"games\".\"developers\" || \"excluded\".\"developers\"))"),
	})
	doUpdates = append(doUpdates, clause.AssignmentColumns([]string{
		"name",
		"storefront",
		"website",
		"verified_developer_usernames",
		"updates",
		"release_date",
		"publisher",
		"total_reviews",
		"positive_reviews",
		"negative_reviews",
		"review_score",
		"total_upvotes",
		"total_downvotes",
		"total_comments",
		"tag_score",
		"weighted_score",
	})...)
	return clause.OnConflict{
		Columns: []clause.Column{{Name: "website"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Not(clause.Expression(clause.Eq{
			Column: "website",
			Value:  nil,
		}))}},
		DoUpdates: doUpdates,
	}
}

// OnCreateOmit returns the fields that should be omitted when creating a Game.
func (g *Game) OnCreateOmit() []string {
	return []string{}
}

// Update will update the Game. It does this by calling the Storefront.ScrapeGame method on the referred to Game.
func (g *Game) Update(db *gorm.DB, config ScrapeConfig) error {
	ScrapeStorefrontForGameModel[string](g.Website.String, g.Wrapper().StorefrontScraper(g.Storefront), config)
	return db.Omit(g.OnCreateOmit()...).Save(g).Error
}

func (g *Game) Wrapper() GameModelWrapper[string, GameModel[string]] { return &GameWrapper{Game: g} }
func (g *Game) GetID() string                                        { return g.Website.String }

type GameWrapper struct{ Game *Game }

func (g *GameWrapper) Default() {
	if g.Game == nil {
		g.Game = &Game{}
	}
	g.Game.Storefront = UnknownStorefront
	g.Game.Name = null.StringFromPtr(nil)
	g.Game.Website = null.StringFromPtr(nil)
	g.Game.Publisher = null.StringFromPtr(nil)
	g.Game.TotalReviews = null.Int32FromPtr(nil)
	g.Game.PositiveReviews = null.Int32FromPtr(nil)
	g.Game.NegativeReviews = null.Int32FromPtr(nil)
	g.Game.ReviewScore = null.Float64FromPtr(nil)
	g.Game.TotalUpvotes = null.Int32FromPtr(nil)
	g.Game.TotalDownvotes = null.Int32FromPtr(nil)
	g.Game.TotalComments = null.Int32FromPtr(nil)
	g.Game.TagScore = null.Float64FromPtr(nil)
	g.Game.ReleaseDate = null.TimeFromPtr(nil)
}

func (g *GameWrapper) Nil() { g.Game = nil }

func (g *GameWrapper) Get() GameModel[string] {
	return g.Game
}

func (g *GameWrapper) StorefrontScraper(storefront Storefront) GameModelStorefrontScraper[string] {
	switch storefront {
	case SteamStorefront:
		return &GameSteamStorefront{Game: g.Game}
	default:
		return UnscrapableError[string](storefront)
	}
}

type GameSteamStorefront GameWrapper

func (g *GameSteamStorefront) Args(url string) []any {
	return SteamStorefront.ScrapeURL().ExtractArgs(url)
}
func (g *GameSteamStorefront) GetStorefront() Storefront { return SteamStorefront }

func (g *GameSteamStorefront) ScrapeInfo(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
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
		g.Game.Name = null.StringFrom(appName.(string))
		log.INFO.Printf("Name of app %v is %s", appID, g.Game.Name.String)

		// steam_release_date is a timestamp as a string. This is our primary way of finding the release date of the
		// Game. However, it sometimes does not appear for unreleased games. In cases where it doesn't appear we will
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
			g.Game.ReleaseDate = null.TimeFrom(time.Unix(appReleaseDateInt, 0))
			log.INFO.Printf("App %v is released/is going to be released on %s", appID, g.Game.ReleaseDate.Time.String())
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
			g.Game.Publisher = null.StringFrom(strings.TrimRight(publisherName.String(), ","))
		}
		return
	})
}

func (g *GameSteamStorefront) ScrapeReviews(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	appID := args[0]
	// Fetch reviews to gather headline stats on the number of reviews
	return browser.SteamAppReviews.RetryJSON(maxTries, minDelay, func(jsonBody map[string]any) error {
		querySummary, ok := jsonBody["query_summary"].(map[string]any)
		if ok {
			g.Game.TotalReviews = null.Int32From(int32(querySummary["total_reviews"].(float64)))
			g.Game.PositiveReviews = null.Int32From(int32(querySummary["total_positive"].(float64)))
			g.Game.NegativeReviews = null.Int32From(int32(querySummary["total_negative"].(float64)))
			log.INFO.Printf(
				"For game %d: total_reviews = %d, positive_reviews = %d, negative_reviews = %d",
				appID, g.Game.TotalReviews.Int32, g.Game.PositiveReviews.Int32, g.Game.NegativeReviews.Int32,
			)
		} else {
			return fmt.Errorf("could not find query_summary key in SteamAppReviews response JSON")
		}
		return nil
	}, appID, "*", "all", 20, "all", "all", -1, -1, "all")
}

func (g *GameSteamStorefront) ScrapeCommunity(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) (err error) {
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

			// Once we know we can get some data we will set the totals fields
			if !g.Game.TotalUpvotes.IsValid() || !g.Game.TotalDownvotes.IsValid() || !g.Game.TotalComments.IsValid() {
				g.Game.TotalUpvotes = null.Int32From(0)
				g.Game.TotalDownvotes = null.Int32From(0)
				g.Game.TotalComments = null.Int32From(0)
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
					g.Game.TotalUpvotes.Int32 += int32(announcementBody["voteupcount"].(float64))
					g.Game.TotalDownvotes.Int32 += int32(announcementBody["votedowncount"].(float64))
					g.Game.TotalComments.Int32 += int32(announcementBody["commentcount"].(float64))
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

func (g *GameSteamStorefront) ScrapeTags(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) (err error) {
	appID := args[0]
	// Use SteamSpy to find the accumulated TagScore for the game
	storefrontConfig := config.ScrapeGetStorefront(SteamStorefront)
	tagConfig := storefrontConfig.StorefrontTags()
	return browser.SteamSpyAppDetails.RetryJSON(maxTries, minDelay, func(jsonBody map[string]any) (err error) {
		// We fall back to SteamSpy for fetching the game's name and publisher if we haven't got them yet
		if !g.Game.Name.IsValid() && !g.Game.Publisher.IsValid() {
			log.INFO.Printf(
				"We have not yet found the name or publisher for appID %d, falling back to SteamSpy",
				appID,
			)
			if name, ok := jsonBody["name"]; ok {
				var nameString string
				if nameString, ok = name.(string); ok {
					g.Game.Name = null.StringFrom(nameString)
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
					g.Game.Publisher = null.StringFrom(publisherName)
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
				g.Game.TagScore = null.Float64From(0.0)
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
				g.Game.TagScore.Float64 += value * upvotesFloat
			}

			// Then we take the average of the score
			if len(tags) > 0 && g.Game.TagScore.Float64 > 0.0 {
				g.Game.TagScore.Float64 = g.Game.TagScore.Float64 / float64(len(tags))
			}
		}
		return
	}, appID)
}

func (g *GameSteamStorefront) ScrapeExtra(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) (err error) {
	appID := args[0]
	// Fetch the game's app page so that we can see if they have included a link to the dev's/game's twitter page. This
	// is only done when there is a Developer for the Game (i.e. the Game's Developers field is not empty). This is
	// also a fallback for finding the release date for games that are unreleased.
	if len(g.Game.Developers) > 0 || !g.Game.ReleaseDate.IsValid() {
		twitterUserURLPattern := regexp.MustCompile(`^https?://(?:www\.)?twitter\.com/(?:#!/)?@?([^/?#]*)(?:[?#].*)?$`)
		return browser.SteamAppPage.RetrySoup(maxTries, minDelay, func(doc *soup.Root) (err error) {
			if len(g.Game.Developers) > 0 {
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

				usernames.Remove("steam")
				g.Game.VerifiedDeveloperUsernames = usernames.ToSlice()
				log.INFO.Printf("Twitter usernames found for %d: %v", appID, usernames)
			}

			if !g.Game.ReleaseDate.IsValid() {
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
					g.Game.ReleaseDate = null.TimeFrom(releaseDate)
					log.INFO.Printf("ReleaseDate found for %d: %s", appID, g.Game.ReleaseDate.Time.String())
				}
			}
			return
		}, appID)
	} else {
		log.WARNING.Printf("Game's Developer field is null, and ReleaseDate has already been found, skipping " +
			"VerifiedDeveloperUsernames scrape...")
	}
	return
}

func (g *GameSteamStorefront) AfterScrape(args ...any) {
	standardisedURL := SteamStorefront.ScrapeURL().Fill(args...)
	g.Game.Storefront = SteamStorefront
	g.Game.Website = null.StringFrom(standardisedURL)
}
