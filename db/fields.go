package db

import (
	"database/sql"
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

// NullDurationFromPtr constructs a new NullDuration from the given pointer to a time.Duration.
func NullDurationFromPtr(duration *time.Duration) NullDuration {
	nd := NullDuration{}
	nd.FromDurationPtr(duration)
	return nd
}
