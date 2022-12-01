package models

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/volatiletech/null/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"reflect"
	"time"
)

// SteamApp represents an app on Steam that has been consumed from the ScoutWebPipes co-process. It can be
// created/updated in the websocket client that reads info pushed from the ScoutWebPipes process.
type SteamApp struct {
	// ID directly matches a Steam app's appID.
	ID uint64
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
	// WeightedScore is a weighted average comprised of the values taken from Updates for the SteamApp. If
	// SteamApp.CheckCalculateWeightedScore is false then this will be nil. This is a computed field, no need to set it
	// before saving.
	WeightedScore null.Float64
}

// steamAppWeight represents a weight for a steamAppWeightedField. If the steamAppWeight is negative then this means to
// take the inverse of the value first, then multiply it by the math.Abs(steamAppWeight).
type steamAppWeight float64

const (
	SteamAppPublisherWeight      steamAppWeight = 0.55
	SteamAppTotalReviewsWeight   steamAppWeight = -0.75
	SteamAppReviewScoreWeight    steamAppWeight = 0.65
	SteamAppTotalUpvotesWeight   steamAppWeight = 0.45
	SteamAppTotalDownvotesWeight steamAppWeight = -0.35
	SteamAppTotalCommentsWeight  steamAppWeight = 0.35
	SteamAppTagScoreWeight       steamAppWeight = 0.25
	SteamAppUpdatesWeight        steamAppWeight = -0.55
	SteamAssetModifiedTimeWeight steamAppWeight = -0.2
)

// steamAppWeightedField represents a field that can have a weighting calculation applied to it in Game.
type steamAppWeightedField string

const (
	SteamAppPublisher      steamAppWeightedField = "Publisher"
	SteamAppTotalReviews   steamAppWeightedField = "TotalReviews"
	SteamAppReviewScore    steamAppWeightedField = "ReviewScore"
	SteamAppTotalUpvotes   steamAppWeightedField = "TotalUpvotes"
	SteamAppTotalDownvotes steamAppWeightedField = "TotalDownvotes"
	SteamAppTotalComments  steamAppWeightedField = "TotalComments"
	SteamAppTagScore       steamAppWeightedField = "TagScore"
	SteamAppUpdates        steamAppWeightedField = "Updates"
	SteamAssetModifiedTime steamAppWeightedField = "AssetModifiedTime"
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
	case SteamAssetModifiedTime:
		w = float64(SteamAssetModifiedTimeWeight)
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
	case SteamAppUpdates:
		return []float64{float64(f.Uint()) / 1500.0}
	case SteamAssetModifiedTime:
		// The value for the asset modified time is calculated by subtracting the time now from the asset modified time,
		// then finding the hours for that duration. We also clamp the duration to be between +/- 5 months.
		timeDiff := f.Interface().(time.Time).Sub(time.Now().UTC())
		if timeDiff.Abs() > time.Hour*24*30*5 {
			timeDiff = time.Hour * 24 * 30 * 5
		}
		return []float64{timeDiff.Hours()}
	default:
		panic(fmt.Errorf("steamAppWeightedField %s is not recognized, and cannot be converted to []float64", sf))
	}
}

func (sf steamAppWeightedField) Fields() []WeightedField {
	return []WeightedField{
		SteamAppUpdates,
		SteamAssetModifiedTime,
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

func (app *SteamApp) CheckCalculateWeightedScore() bool {
	return true
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

func (s SteamAppSteamStorefront) Args(id uint64) []any      { return []any{id} }
func (s SteamAppSteamStorefront) GetStorefront() Storefront { return SteamStorefront }

func (s SteamAppSteamStorefront) ScrapeInfo(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	//TODO implement me
	panic("implement me")
}

func (s SteamAppSteamStorefront) ScrapeReviews(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	//TODO implement me
	panic("implement me")
}

func (s SteamAppSteamStorefront) ScrapeTags(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	//TODO implement me
	panic("implement me")
}

func (s SteamAppSteamStorefront) ScrapeCommunity(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	//TODO implement me
	panic("implement me")
}

func (s SteamAppSteamStorefront) ScrapeExtra(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	//TODO implement me
	panic("implement me")
}

func (s SteamAppSteamStorefront) AfterScrape(args ...any) {}
