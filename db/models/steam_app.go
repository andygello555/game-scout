package models

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/anaskhan96/soup"
	myErrors "github.com/andygello555/agem"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/monday"
	"github.com/andygello555/gapi"
	"github.com/andygello555/go-steamcmd"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	"github.com/volatiletech/null/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func init() {
	gob.Register(&SteamApp{})
}

// SteamApp represents an app on Steam that has been consumed from the ScoutWebPipes co-process. It can be
// created/updated in the websocket client that reads info pushed from the ScoutWebPipes process.
type SteamApp struct {
	// ID directly matches a Steam app's appID.
	ID uint64
	// Name is the current name of the SteamApp.
	Name string
	// MondayItemID is a non-database field that is filled when fetching the SteamApp from Monday using the
	// GetSteamAppsFromMonday monday.Binding.
	MondayItemID int `gorm:"-:all"`
	// MondayBoardID is a non-database field that is filled when fetching the SteamApp from Monday using the
	// GetSteamAppsFromMonday monday.Binding.
	MondayBoardID string `gorm:"-:all"`
	// Type is only used for our benefit to check what type of software this SteamApp is. We do not save it to the
	// database at all, because we only save SteamApp's that are games.
	Type string `gorm:"-:all"`
	// CreatedAt is when this SteamApp was first created. SteamApp's that were CreatedAt closer to the current update
	// time are desired more than SteamApp's that were CreatedAt further in the past.
	CreatedAt time.Time `gorm:"<-:create"`
	// Bare is set when the SteamApp is bare version of the app fetched from SteamGetAppList. When set, the SteamApp
	// will only have ID and Name set to not be default.
	Bare bool
	// Updates is the number of times this SteamApp has been updated in the DB. This is not the number of changelist
	// that have occurred for this title.
	Updates uint64
	// DeveloperID is the foreign key to a Developer on Twitter. This can be nil/null, in case there is no socials
	// linked on the SteamApp's store page.
	DeveloperID *string
	Developer   *Developer `gorm:"constraint:OnDelete:SET NULL;"`
	// ReleaseDate is when this game was/is going to be released on SteamStorefront.
	ReleaseDate time.Time
	// OwnersOnly indicates whether the games details can only be seen by owners of the game. If this is true then it
	// negatively impacts this SteamApp's WeightedScore.
	OwnersOnly bool
	// HasStorepage indicates whether this game has a storepage on Steam. If this is false then it negatively impacts
	// this SteamApp's WeightedScore.
	HasStorepage bool
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
	// TimesHighlighted is the number of times this SteamApp has been highlighted by the measure command.
	TimesHighlighted int32
	// Watched indicates the Monday.com item Id, from the connected Monday board, for the SteamApp. If this is nil, then
	// it can be assumed that this SteamApp is not being watched.
	Watched *string `gorm:"default:null;type:varchar(32)"`
	// Votes is the number of upvotes for the SteamApp on the linked Monday board/group minus the number of downvotes.
	Votes int32 `gorm:"default:0"`
	// WeightedScore is a weighted average comprised of the values taken from CreatedAt, Updates, ReleaseDate,
	// Publisher, TotalReviews, ReviewScore, TotalUpvotes, TotalDownvotes, TotalComments, TagScore, AssetModifiedTime,
	// and TimesHighlighted for the SteamApp. If SteamApp.CheckCalculateWeightedScore is false then this will be nil.
	// This is a computed field, no need to set it before saving.
	WeightedScore null.Float64
}

func (app *SteamApp) String() string {
	return fmt.Sprintf("\"%s\" (%d)", app.Name, app.ID)
}

func (app *SteamApp) Empty() any {
	return &SteamApp{}
}

func (app *SteamApp) Order() string {
	return "id"
}

// UpdateComputedFields will update the fields in a SteamApp that are computed.
func (app *SteamApp) UpdateComputedFields(tx *gorm.DB) (err error) {
	// Calculate the ReviewScore
	// Note: unlike Game we can safely always calculate the ReviewScore
	if app.TotalReviews == 0 {
		app.ReviewScore = 1.0
	} else {
		app.ReviewScore = float64(app.PositiveReviews) / float64(app.TotalReviews)
	}

	// Calculate the WeightedScore
	app.WeightedScore = null.Float64FromPtr(nil)
	if app.CheckCalculateWeightedScore() {
		var weightedScore float64
		if weightedScore, err = scrapeConfig.ScrapeWeightedModelCalc(app); err != nil {
			return
		}
		app.WeightedScore = null.Float64From(weightedScore)
	}
	return
}

// CheckCalculateWeightedScore will only return true when the SteamApp is not Bare, has no Publisher, and has a name.
// This is to stop the calculation of weights for games that have not yet been scraped/aren't fully initialised by
// Steam's backend. CreateInitialSteamApps will create SteamApp(s) with their Bare field set.
func (app *SteamApp) CheckCalculateWeightedScore() bool {
	return app.Name != "" && !app.Bare && !app.Publisher.IsValid()
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

// OnConflict returns the clause.OnConflict that should be checked in an upsert clause. For SteamApp the
// clause.OnConflict.DoUpdates field will be set as the Updates will need to be incremented and the CreatedAt field
// excluded from being updated.
func (app *SteamApp) OnConflict() clause.OnConflict {
	doUpdates := clause.Assignments(map[string]interface{}{"updates": gorm.Expr("\"excluded\".\"updates\" + 1")})
	doUpdates = append(doUpdates, clause.AssignmentColumns([]string{
		"name",
		"bare",
		"developer_id",
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
		"asset_modified_time",
		"last_changelist_id",
		"times_highlighted",
		"weighted_score",
	})...)
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: doUpdates,
	}
}

// OnCreateOmit returns the fields that should be omitted when creating a SteamApp.
func (app *SteamApp) OnCreateOmit() []string {
	return []string{"Developer", "CreatedAt"}
}

func (app *SteamApp) Update(db *gorm.DB, config ScrapeConfig) error {
	ScrapeStorefrontForGameModel[uint64](app.ID, app.Wrapper().StorefrontScraper(SteamStorefront), config)
	return db.Omit(app.OnCreateOmit()...).Save(app).Error
}

func (app *SteamApp) Website() string {
	return browser.SteamAppPage.Fill(app.ID)
}

// GetSteamAppsFromMonday is a api.Binding that retrieves all the SteamApp from the mapped board and group. Arguments
// provided to execute:
//
// • page (int): The page of results to retrieve. This means that GetSteamAppsFromMonday can be passed to an
// api.Paginator.
//
// • config (monday.Config): The monday.Config to use to find the monday.MappingConfig for the SteamApp model.
//
// • db (*gorm.DB): The gorm.DB instance to use to search for the SteamApp's of the IDs found in the Monday items. This
// is only used in the Response method.
//
// Execute returns a list of SteamApp instances within their mapped board and group combination for the given page of
// results. It does this by retrieving the SteamApp.ID from the appropriate column from each item and then searching the
// gorm.DB instance which is provided in the 3rd argument.
var GetSteamAppsFromMonday = api.NewBinding[monday.ItemResponse, []*SteamApp](
	func(b api.Binding[monday.ItemResponse, []*SteamApp], args ...any) api.Request {
		page := args[0].(int)
		mapping := args[1].(monday.Config).MondayMappingForModel(SteamApp{})
		boardIds := mapping.MappingBoardIDs()
		groupIds := mapping.MappingGroupIDs()
		return monday.GetItems.Request(page, boardIds, groupIds)
	},
	monday.ResponseWrapper[monday.ItemResponse, []*SteamApp],
	monday.ResponseUnwrapped[monday.ItemResponse, []*SteamApp],
	func(b api.Binding[monday.ItemResponse, []*SteamApp], response monday.ItemResponse, args ...any) []*SteamApp {
		items := monday.GetItems.Response(response)
		mapping := args[1].(monday.Config).MondayMappingForModel(SteamApp{})
		db := args[2].(*gorm.DB)
		apps := make([]*SteamApp, 0)
		for _, item := range items {
			columnMap := make(map[string]monday.ColumnValue)
			for _, column := range item.ColumnValues {
				columnMap[column.Id] = column
			}

			var (
				appID  uint64
				column monday.ColumnValue
				err    error
				ok     bool
			)
			if column, ok = columnMap[mapping.MappingModelInstanceIDColumnID()]; !ok {
				continue
			}

			app := SteamApp{}
			if appID, err = strconv.ParseUint(strings.Trim(column.Value, `"`), 10, 64); err != nil {
				continue
			}

			if err := db.Find(&app, "id = ?", appID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}

			apps = append(apps, &app)

			var itemID int64
			if itemID, err = strconv.ParseInt(item.Id, 10, 64); err == nil {
				app.MondayItemID = int(itemID)
			}
			app.MondayBoardID = item.BoardId

			if column, ok = columnMap[mapping.MappingModelInstanceWatchedColumnID()]; ok {
				if column.Value != "null" {
					var watches monday.Votes
					if err = json.Unmarshal([]byte(column.Value), &watches); err == nil {
						if len(watches.VoterIds) > 0 {
							app.Watched = &item.Id
						}
					}
				} else {
					app.Watched = nil
				}
			}

			voteValues := []int{0, 0}
			for i, votesColumnID := range []string{
				mapping.MappingModelInstanceUpvotesColumnID(),
				mapping.MappingModelInstanceDownvotesColumnID(),
			} {
				var voteColumn monday.ColumnValue
				if voteColumn, ok = columnMap[votesColumnID]; !ok {
					continue
				}
				if voteColumn.Value == "null" {
					continue
				}
				var votes monday.Votes
				if err = json.Unmarshal([]byte(voteColumn.Value), &votes); err != nil {
					continue
				}
				voteValues[i] = len(votes.VoterIds)
			}
			app.Votes = int32(voteValues[0] - voteValues[1])
		}
		return apps
	}, func(binding api.Binding[monday.ItemResponse, []*SteamApp]) []api.BindingParam {
		return api.Params(
			"page", 0, true,
			"config", reflect.TypeOf((*monday.Config)(nil)), true,
			"db", &gorm.DB{}, true,
		)
	}, true,
	func(client api.Client) (string, any) { return "jsonResponseKey", "boards" },
	func(client api.Client) (string, any) { return "config", client.(*monday.Client).Config },
).SetName("GetSteamAppsFromMonday")

// AddSteamAppToMonday adds a SteamApp to the mapped board and group by constructing column values using the
// monday.MappingConfig.ColumnValues method for the monday.MappingConfig for SteamApp. Arguments provided to Execute:
//
// • game (*SteamApp): The SteamApp to add to the mapped board and group combination.
//
// • config (monday.Config): The monday.Config used to fetch the monday.MappingConfig for SteamApp from. This
// monday.MappingConfig is then used to generate the column values that are posted to the Monday API to construct a new
// item on the mapped board and group combination.
//
// Execute returns the item ID of the newly created item. This can then be used to set the SteamApp.Watched field
// appropriately if necessary.
var AddSteamAppToMonday = api.NewBinding[monday.ItemId, string](
	func(b api.Binding[monday.ItemId, string], args ...any) api.Request {
		app := args[0].(*SteamApp)
		itemName := app.Name
		mapping := args[1].(monday.Config).MondayMappingForModel(SteamApp{})
		columnValues, err := mapping.ColumnValues(app)
		if err != nil {
			panic(err)
		}
		return monday.AddItem.Request(
			mapping.MappingBoardIDs()[0],
			mapping.MappingGroupIDs()[0],
			itemName,
			columnValues,
		)
	},
	monday.ResponseWrapper[monday.ItemId, string],
	monday.ResponseUnwrapped[monday.ItemId, string],
	monday.AddItem.GetResponseMethod(),
	func(binding api.Binding[monday.ItemId, string]) []api.BindingParam {
		return api.Params(
			"game", &SteamApp{}, true,
			"config", reflect.TypeOf((*monday.Config)(nil)), true,
		)
	}, false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "create_item" },
	func(client api.Client) (string, any) { return "config", client.(*monday.Client).Config },
).SetName("AddSteamAppToMonday")

// UpdateSteamAppInMonday is a api.Binding which updates the monday.Item of the given ID within the monday.Board of
// the given ID for SteamApp using the monday.MappingConfig.ColumnValues method to generate values for all the
// monday.Column IDs provided by the monday.MappingConfig.MappingColumnsToUpdate method. Arguments provided to Execute:
//
// • game (*SteamApp): The SteamApp to use as the basis for the new column values.
//
// • itemId (int): The ID of the monday.Item that represents the given SteamApp.
//
// • boardId (int): The ID of the monday.Board within which the monday.Item resides.
//
// • config (monday.Config): The monday.Config used to fetch the monday.MappingConfig for SteamApp from. This
// monday.MappingConfig is then used to generate the column values that are posted to the Monday API to mutate the column
// values of the monday.Item of the given ID in the mapped monday.Board.
//
// Execute returns the ID of the monday.Item that has been mutated.
var UpdateSteamAppInMonday = api.NewBinding[monday.ItemId, string](
	func(b api.Binding[monday.ItemId, string], args ...any) api.Request {
		app := args[0].(*SteamApp)
		itemId := args[1].(int)
		boardId := args[2].(int)
		mapping := args[3].(monday.Config).MondayMappingForModel(SteamApp{})
		columnValues, err := mapping.ColumnValues(app, mapping.MappingColumnsToUpdate()...)
		if err != nil {
			panic(err)
		}
		return monday.ChangeMultipleColumnValues.Request(
			itemId,
			boardId,
			columnValues,
		)
	},
	monday.ResponseWrapper[monday.ItemId, string],
	monday.ResponseUnwrapped[monday.ItemId, string],
	monday.ChangeMultipleColumnValues.GetResponseMethod(),
	func(binding api.Binding[monday.ItemId, string]) []api.BindingParam {
		return api.Params(
			"game", &SteamApp{}, true,
			"itemId", 0, true,
			"boardId", 0, true,
			"config", reflect.TypeOf((*monday.Config)(nil)), true,
		)
	}, false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "change_multiple_column_values" },
	func(client api.Client) (string, any) { return "config", client.(*monday.Client).Config },
).SetName("UpdateSteamAppInMonday")

// VerifiedDeveloper returns the first verified Developer for this SteamApp. If there is not one, we will return a nil pointer.
func (app *SteamApp) VerifiedDeveloper(db *gorm.DB) *Developer {
	if app.DeveloperID == nil {
		return nil
	}
	return app.Developer
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
	s.SteamApp.Bare = false
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
			appType           any
			appName           any
			appAssetMTime     string
			appAssetMTimeInt  int64
			appSectionType    string
			appReleaseDate    string
			appReleaseDateInt int64
			associations      map[string]any
		)

		if jsonBody, ok = cmd.ParsedOutputs[0].(map[string]any); !ok {
			return fmt.Errorf("cannot assert parsed output of app_info_print for %v to map", appID)
		}

		// If the len of the JSONBody is zero then we will do a quick continue to the next try
		if len(jsonBody) == 0 {
			log.INFO.Printf(
				"SteamCMD \"app_info_print %d\". Output has an empty body, presuming that game doesn't fully exist...",
				appID,
			)
			return myErrors.Continue
		}

		if appDetails, ok = jsonBody["common"].(map[string]any); !ok {
			return fmt.Errorf("cannot get \"common\" from parsed output for %v", appID)
		}

		if appType, ok = appDetails["type"]; !ok {
			return fmt.Errorf("cannot find \"type\" key in common details for %d", appID)
		}
		s.SteamApp.Type = strings.ToLower(appType.(string))
		log.INFO.Printf("SteamApp %d has type: %s", appID, s.SteamApp.Type)

		// We exit out of the scrape if the SteamApp is not a Game. This is so we can not waste anymore time with app.
		if s.SteamApp.Type != "game" {
			log.WARNING.Printf("SteamApp %d is not a Game (it is a \"%s\"). Skipping ScrapeInfo...", appID, s.SteamApp.Type)
			return myErrors.Done
		}

		if appName, ok = appDetails["name"]; !ok {
			return fmt.Errorf("cannot find \"name\" key in common details for %v", appID)
		}
		s.SteamApp.Name = appName.(string)
		log.INFO.Printf("Name of app %v is \"%s\"", appID, s.SteamApp.Name)

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

		// Check if this game is visible to owners only
		s.SteamApp.OwnersOnly = false
		if appSectionType, ok = appDetails["section_type"].(string); ok {
			s.SteamApp.OwnersOnly = appSectionType == "ownersonly"
			log.INFO.Printf(
				"SteamApp %d has a defined section_type = %s, OwnersOnly = %t",
				appID, appSectionType, s.SteamApp.OwnersOnly,
			)
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

		developers, publishers := mapset.NewThreadUnsafeSet[string](), mapset.NewThreadUnsafeSet[string]()
		for i, association := range associations {
			var associationMap map[string]any
			if associationMap, ok = association.(map[string]any); !ok {
				return fmt.Errorf("association %s in details for %v is not a map", i, appID)
			}

			name := associationMap["name"].(string)
			switch associationMap["type"].(string) {
			case "developer":
				developers.Add(name)
			case "publisher":
				publishers.Add(name)
			default:
				break
			}
		}

		log.INFO.Printf("Publisher(s) for %v: %s", appID, publishers.String())
		log.INFO.Printf("Developer(s) for %v: %s", appID, developers.String())
		var uniquePublishers mapset.Set[string]
		if uniquePublishers = publishers.Difference(developers); uniquePublishers.Cardinality() > 0 {
			s.SteamApp.Publisher = null.StringFrom(strings.Join(uniquePublishers.ToSlice(), ","))
		}
		log.INFO.Printf(
			"Unique publisher(s) for %v: %s, Publisher.IsValid = %t",
			appID, uniquePublishers.String(), s.SteamApp.Publisher.IsValid(),
		)
		return
	})
}

func (s *SteamAppSteamStorefront) ScrapeReviews(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	appID := args[0]
	if s.SteamApp.Type != "game" {
		return fmt.Errorf("%d is not a Game (it is a \"%s\"). Skipping ScrapeReviews", appID, s.SteamApp.Type)
	}

	// Fetch reviews to gather headline stats on the number of reviews
	return browser.SteamAppReviews.RetryJSON(nil, maxTries, minDelay, func(jsonBody map[string]any, resp *http.Response) error {
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
	if s.SteamApp.Type != "game" {
		return fmt.Errorf("%d is not a Game (it is a \"%s\"). Skipping ScrapeCommunity", appID, s.SteamApp.Type)
	}

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
			if jsonBody, _, err = browser.SteamCommunityPosts.JSON(nil, appID, 0, batchSize, gidEvent, gidAnnouncement); err != nil {
				log.WARNING.Printf("Could not get SteamCommunityPosts JSON for %d: %s. Tries left: %d", appID, err.Error(), tries)
				if tries > 0 {
					tries--
					time.Sleep(minDelay * time.Duration(maxTries+1-tries))
					return con
				}
				return brk
			}

			if _, ok := jsonBody["err_msg"]; jsonBody["success"].(float64) == 42 && ok {
				log.INFO.Printf(
					"SteamCommunityPosts response for %d has error code 42 and an error message. Assuming community "+
						"hub doesn't exist for the game.",
					appID,
				)
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
	if s.SteamApp.Type != "game" {
		return fmt.Errorf("%d is not a Game (it is a \"%s\"). Skipping ScrapeTags", appID, s.SteamApp.Type)
	}

	// Use SteamSpy to find the accumulated TagScore for the game
	storefrontConfig := config.ScrapeGetStorefront(SteamStorefront)
	tagConfig := storefrontConfig.StorefrontTags()
	return browser.SteamSpyAppDetails.RetryJSON(nil, maxTries, minDelay, func(jsonBody map[string]any, resp *http.Response) (err error) {
		// We fall back to SteamSpy for fetching the game's name and publisher if we haven't got them yet
		if s.SteamApp.Name == "" || !s.SteamApp.Publisher.IsValid() {
			log.INFO.Printf(
				"We have not yet found the name or publisher for appID %d, falling back to SteamSpy",
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
	if s.SteamApp.Type != "game" {
		return fmt.Errorf("%d is not a Game (it is a \"%s\"). Skipping ScrapeExtra", appID, s.SteamApp.Type)
	}

	twitterUserURLPattern := regexp.MustCompile(`^https?://(?:www\.)?twitter\.com/(?:#!/)?@?([^/?#]*)(?:[?#].*)?$`)

	// We will get around the agecheck by setting the following cookies
	var req *http.Request
	if _, req, err = browser.SteamAppPage.GetRequest(appID); err != nil {
		return
	}
	req.AddCookie(&http.Cookie{Name: "birthtime", Value: "568022401"})
	req.AddCookie(&http.Cookie{Name: "mature_content", Value: "1"})

	return browser.SteamAppPage.RetrySoup(req, maxTries, minDelay, func(doc *soup.Root, resp *http.Response) (err error) {
		url := resp.Request.URL.String()
		if s.SteamApp.HasStorepage, err = regexp.MatchString(`^https?://store\.steampowered\.com/app/\d+(/\w+)?/?$`, url); err != nil {
			err = errors.Wrapf(err, "could not execute regex on URL %s", url)
			return
		}
		log.INFO.Printf(
			"When requesting URL \"%s\" for SteamApp %d we are redirected to \"%s\"",
			browser.SteamAppPage.Fill(appID), appID, url,
		)

		// If the SteamApp doesn't have a storepage, we don't bother continuing
		if !s.SteamApp.HasStorepage {
			log.INFO.Printf("Redirected to \"%s\". App %d probably doesn't exist...", url, appID)
			return myErrors.Done
		}
		log.INFO.Printf("SteamApp \"%s\" (%d) has a storepage!", s.SteamApp.Name, appID)

		if !s.SteamApp.Publisher.IsValid() {
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
			log.INFO.Printf(
				"Found the following Twitter users on the storepage for SteamApp %d: %s",
				appID, usernames.String(),
			)
			if usernames.Cardinality() > 0 {
				username, _ := usernames.Pop()
				log.INFO.Printf("Passing back Twitter username \"%s\" found for %d", username, appID)
				s.SteamApp.Developer = &Developer{Username: username}
			} else {
				log.INFO.Printf(
					"SteamAppPage for %d now contains no Twitter usernames after removing \"Steam\" social. "+
						"Skipping fetch for Twitter username...",
					appID,
				)
			}
		} else {
			log.INFO.Printf("Skipping Twitter API lookup of devs for %d because they already have a publisher", appID)
		}

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
		} else {
			log.INFO.Printf(
				"Skipping fetching release date for %d as we already have a non-zero release date (%s)",
				appID, s.SteamApp.ReleaseDate.String(),
			)
		}
		return
	}, appID)
}

func (s *SteamAppSteamStorefront) AfterScrape(args ...any) {
	// AfterScrape just sets the ID to the first arg which should be the appID of the SteamApp.
	s.SteamApp.ID = args[0].(uint64)
}

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
		if err = browser.SteamGetAppList.RetryJSON(nil, 3, time.Second*20, func(jsonBody map[string]any, resp *http.Response) (err error) {
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
				app := &SteamApp{Publisher: null.StringFromPtr(nil), Updates: 0, Bare: true}
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
		if err = db.CreateInBatches(&steamApps, 100).Error; err != nil {
			err = errors.Wrapf(err, "could not create %d initial SteamApps", len(steamApps))
		}
	} else {
		log.WARNING.Printf("There are already %d SteamApps in the DB, skipping initial creation...", count)
	}
	return
}
