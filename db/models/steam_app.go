package models

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/volatiletech/null/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"reflect"
)

// SteamApp represents an app on Steam that has been consumed from the ScoutWebPipes co-process. It can be
// created/updated in the websocket client that reads info pushed from the ScoutWebPipes process.
type SteamApp struct {
	// ID directly matches a Steam app's appID.
	ID uint64
	// Updates is the number of times this SteamApp has been updated in the DB. This is not the number of changelist
	// that have occurred for this title.
	Updates uint64
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
	UpdatesWeight steamAppWeight = -0.55
)

// steamAppWeightedField represents a field that can have a weighting calculation applied to it in Game.
type steamAppWeightedField string

const (
	Updates steamAppWeightedField = "Updates"
)

// String returns the string value of the gameWeightedField.
func (sf steamAppWeightedField) String() string { return string(sf) }

// Weight returns the steamAppWeight for a steamAppWeightedField, as well as whether the value should have its inverse
// taken first.
func (sf steamAppWeightedField) Weight() (w float64, inverse bool) {
	switch sf {
	case Updates:
		w = float64(UpdatesWeight)
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
	case Updates:
		return []float64{float64(f.Int())}
	default:
		panic(fmt.Errorf("steamAppWeightedField %s is not recognized, and cannot be converted to []float64", sf))
	}
}

func (sf steamAppWeightedField) Fields() []WeightedField {
	return []WeightedField{
		Updates,
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
		app.WeightedScore = null.Float64From(CalculateWeightedScore(app, Updates))
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
	return []string{}
}
