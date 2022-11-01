package models

import (
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"gorm.io/gorm"
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
	// Disabled represents whether this Developer should be included in the Update phase of the Scout procedure.
	Disabled bool
}

// LatestDeveloperSnapshot will get the latest DeveloperSnapshot for this Developer.
func (d *Developer) LatestDeveloperSnapshot(db *gorm.DB) (developerSnap *DeveloperSnapshot, err error) {
	developerSnap = &DeveloperSnapshot{}
	err = db.Model(&DeveloperSnapshot{}).Where("developer_id = ?", d.ID).Order("version desc").Limit(1).First(developerSnap).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		err = errors.Wrapf(err, "could not find the latest DeveloperSnapshot for %s", d.ID)
	}
	return
}

// OnConflict returns the clause.OnConflict that should be checked in an upsert clause.
func (d *Developer) OnConflict() clause.OnConflict {
	return clause.OnConflict{Columns: []clause.Column{{Name: "id"}}}
}
