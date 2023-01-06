package models

import (
	"fmt"
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
	// TimesHighlighted is the number of times this Developer has been highlighted by the Measure phase.
	TimesHighlighted int32
	// Disabled represents whether this Developer should be included in the Update phase of the Scout procedure.
	Disabled bool
}

func (d *Developer) GetObservedName() string { return "Snapshot Weighted Score" }

func (d *Developer) GetVariableNames() []string { return []string{"Snapshot Date"} }

func (d *Developer) Train(trend *Trend) (err error) {
	var snapshots []*DeveloperSnapshot
	if snapshots, err = d.DeveloperSnapshots(trend.db); err != nil {
		return errors.Wrapf(err, "cannot Train %s (%s) as we cannot find snapshots for it", d.Username, d.ID)
	}
	if len(snapshots) < 2 {
		return fmt.Errorf("cannot Train on %d datapoints for Developer %s (%s)", len(snapshots), d.Username, d.ID)
	}

	for _, snapshot := range snapshots {
		trend.AddDataPoint(snapshot.CreatedAt, snapshot.WeightedScore)
	}
	return
}

// DeveloperSnapshots will find all the DeveloperSnapshot for this Developer. The resulting array is ordered by the
// DeveloperSnapshot.Version ascending.
func (d *Developer) DeveloperSnapshots(db *gorm.DB) (developerSnapshots []*DeveloperSnapshot, err error) {
	err = db.Model(&DeveloperSnapshot{}).Where("developer_id = ?", d.ID).Order("version").Find(&developerSnapshots).Error
	return
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

// Games returns the games for this developer ordered by weighted score descending.
func (d *Developer) Games(db *gorm.DB) (games []*Game, err error) {
	if err = db.Model(&Game{}).Where("? = ANY(developers)", d.Username).Order("weighted_score desc").Find(&games).Error; err != nil {
		return games, errors.Wrapf(err, "could not find Games for %s (%s)", d.Username, d.ID)
	}
	return
}

// OnConflict returns the clause.OnConflict that should be checked in an upsert clause.
func (d *Developer) OnConflict() clause.OnConflict {
	return clause.OnConflict{Columns: []clause.Column{{Name: "id"}}}
}

// OnCreateOmit returns the fields that should be omitted when creating a Developer.
func (d *Developer) OnCreateOmit() []string {
	return []string{}
}

func (d *Developer) Trend(db *gorm.DB) (trend *Trend, err error) {
	trend = NewTrend(db, d)
	if err = trend.Train(); err != nil {
		err = errors.Wrapf(err, "Trend could not be trained for Developer %s (%s)", d.Username, d.ID)
		return
	}

	if _, err = trend.Trend(); err != nil {
		err = errors.Wrapf(err, "Trend could not be run for Developer %s (%s)", d.Username, d.ID)
		return
	}
	return
}
