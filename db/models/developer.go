package models

import (
	"github.com/g8rswimmer/go-twitter/v2"
	"gorm.io/gorm/clause"
	"time"
)

// Developer represents a potential indie developer's Twitter account. This also contains some current metrics for their
// profile.
type Developer struct {
	// ID is the ID of the Twitter user that this developer corresponds to.
	ID string
	// Name is the name of the Twitter user that this developer corresponds to.
	Name string
	// Username is the username of the Twitter user that this developer corresponds to.
	Username string
	// Description is the bio of the Twitter user that this developer corresponds to.
	Description string
	// ProfileCreated is when the Developer's Twitter profile was created. Note how we avoid using CreatedAt.
	ProfileCreated time.Time
	// PublicMetrics is the most up-to-date public metrics for this Developer's Twitter profile.
	PublicMetrics *twitter.UserMetricsObj `gorm:"embedded;embeddedPrefix:current_"`
	// UpdatedAt is when this developer was updated. So we know up until when the PublicMetrics are fresh to.
	UpdatedAt time.Time
}

// OnConflict returns the clause.OnConflict that should be checked in an upsert clause.
func (d *Developer) OnConflict() clause.OnConflict {
	return clause.OnConflict{Columns: []clause.Column{{Name: "id"}}}
}
