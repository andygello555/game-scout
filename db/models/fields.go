package models

import (
	"database/sql"
	"gorm.io/gorm/callbacks"
	"time"
)

// NullDuration represents a nullable version of time.Duration.
type NullDuration struct {
	sql.NullInt64
}

// FromDurationPtr sets the referred to NullDuration to the given time.Duration, setting NullDuration.NullInt64.Valid
// appropriately.
func (nd *NullDuration) FromDurationPtr(duration *time.Duration) {
	*nd = NullDuration{
		sql.NullInt64{},
	}
	nd.NullInt64.Valid = duration != nil
	if nd.NullInt64.Valid {
		nd.NullInt64.Int64 = int64(*duration)
	}
}

func (nd *NullDuration) IsValid() bool {
	return nd.Valid
}

func (nd *NullDuration) Ptr() *time.Duration {
	if !nd.IsValid() {
		return nil
	}
	duration := time.Duration(nd.NullInt64.Int64)
	return &duration
}

// NullDurationFrom constructs a new NullDuration from the given time.Duration. This will never be null.
func NullDurationFrom(duration time.Duration) NullDuration {
	nd := NullDuration{}
	nd.FromDurationPtr(&duration)
	return nd
}

// NullDurationFromPtr constructs a new NullDuration from the given pointer to a time.Duration.
func NullDurationFromPtr(duration *time.Duration) NullDuration {
	nd := NullDuration{}
	nd.FromDurationPtr(duration)
	return nd
}

type WeightedModel interface {
	callbacks.BeforeCreateInterface
	callbacks.BeforeUpdateInterface
}

type CheckedWeightedModel interface {
	WeightedModel
	CheckCalculateWeightedScore() bool
}
