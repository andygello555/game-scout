package models

import (
	"database/sql/driver"
	"encoding/gob"
	"fmt"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

func init() {
	gob.Register(Developer{})
	gob.Register(&Developer{})
	gob.Register(TrendingDev{})
	gob.Register(&TrendingDev{})
}

type DeveloperType string

const (
	UnknownDeveloperType DeveloperType = "U"
	TwitterDeveloperType DeveloperType = "T"
	RedditDeveloperType  DeveloperType = "R"
)

// DevTypeFromUsername returns the DeveloperType of the given Developer.Username, as well as the given
// Developer.Username without the prefixed DeveloperType character.
func DevTypeFromUsername(username string) (DeveloperType, string) {
	for _, devType := range UnknownDeveloperType.Values() {
		if strings.HasPrefix(username, devType) {
			return DeveloperType(devType), strings.TrimPrefix(username, devType)
		}
	}
	return UnknownDeveloperType, username
}

func (dt DeveloperType) EnumValue() string {
	return string(dt)
}

// Index returns the index of the DeveloperType.
func (dt DeveloperType) Index() int {
	switch dt {
	case TwitterDeveloperType:
		return 1
	case RedditDeveloperType:
		return 2
	default:
		return 0
	}
}

// String returns the formal name of the DeveloperType.
func (dt DeveloperType) String() string {
	switch dt {
	case UnknownDeveloperType:
		return "Unknown"
	case TwitterDeveloperType:
		return "Twitter"
	case RedditDeveloperType:
		return "Reddit"
	default:
		return "<nil>"
	}
}

func (dt *DeveloperType) Scan(value interface{}) error {
	switch value.(type) {
	case []byte:
		*dt = DeveloperType(value.([]byte))
	case string:
		*dt = DeveloperType(value.(string))
	default:
		panic(fmt.Errorf("could not convert DB value to DeveloperType"))
	}
	return nil
}

func (dt DeveloperType) Value() (driver.Value, error) {
	return string(dt), nil
}

func (dt DeveloperType) Type() string {
	return "developer_type"
}

func (dt DeveloperType) Values() []string {
	return []string{
		string(UnknownDeveloperType),
		string(TwitterDeveloperType),
		string(RedditDeveloperType),
	}
}

func (dt DeveloperType) Types() []DeveloperType {
	return []DeveloperType{
		TwitterDeveloperType,
		RedditDeveloperType,
	}
}

type RedditUserMetrics struct {
	PostKarma    int
	CommentKarma int
}

// DeveloperMinimal is used within the state cache to save details for a developer that has been updated, disabled, or
// enabled.
type DeveloperMinimal struct {
	// ID is the ID of the Twitter user that this developer corresponds to.
	ID string
	// Type denotes whether we found this Developer on Twitter or Reddit.
	Type DeveloperType
}

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
	// Type denotes whether we found this Developer on Twitter or Reddit.
	Type DeveloperType `gorm:"type:developer_type"`
	// ProfileCreated is when the Developer's Twitter profile was created. Note how we avoid using CreatedAt.
	ProfileCreated time.Time
	// PublicMetrics is the most up-to-date public metrics for this Developer's Twitter profile.
	PublicMetrics *twitter.UserMetricsObj `gorm:"embedded;embeddedPrefix:current_"`
	// RedditPublicMetrics contains the user's karma if they are a RedditDeveloperType Developer.
	RedditPublicMetrics *RedditUserMetrics `gorm:"embedded;embeddedPrefix:current_reddit_"`
	// UpdatedAt is when this developer was updated. So we know up until when the PublicMetrics are fresh to.
	UpdatedAt time.Time
	// TimesHighlighted is the number of times this Developer has been highlighted by the Measure phase.
	TimesHighlighted int32
	// Disabled represents whether this Developer should be included in the Update phase of the Scout procedure.
	Disabled bool
}

// String returns a string representation of the Developer's Username, ID, and Disabled fields.
func (d *Developer) String() string {
	return fmt.Sprintf("\"%s\" (%s, %s, %s)", d.Username, d.ID, d.Type.String(), map[bool]string{
		true:  "disabled",
		false: "enabled",
	}[d.Disabled])
}

// Link returns the URL link to the Developer's Twitter page.
func (d *Developer) Link() string {
	return fmt.Sprintf("https://twitter.com/%s", d.Username)
}

// TypedUsername returns the Developer's Username prefixed with the string representation of Type.
func (d *Developer) TypedUsername() string { return fmt.Sprintf("%s%s", string(d.Type), d.Username) }

func (d *Developer) GetObservedName() string { return "Snapshot Weighted Score" }

func (d *Developer) GetVariableNames() []string { return []string{"Snapshot Date"} }

func (d *Developer) Train(trend *Trend) (err error) {
	var snapshots []*DeveloperSnapshot
	if snapshots, err = d.DeveloperSnapshots(trend.db); err != nil {
		return errors.Wrapf(err, "cannot Train %v as we cannot find snapshots for it", d)
	}
	if len(snapshots) < 2 {
		return fmt.Errorf("cannot Train on %d datapoints for Developer %v", len(snapshots), d)
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
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = errors.Wrapf(err, "could not find the latest DeveloperSnapshot for %s", d.ID)
	}
	return
}

// Games returns the games for this developer ordered by weighted score descending.
func (d *Developer) Games(db *gorm.DB) (games []*Game, err error) {
	if err = db.Model(&Game{}).Where("? = ANY(developers)", d.TypedUsername()).Order("weighted_score desc").Find(&games).Error; err != nil {
		return games, errors.Wrapf(err, "could not find Games for %v", d)
	}
	return
}

// Trend returns the Trend that has been trained on the DeveloperSnapshot for the referred to Developer.
func (d *Developer) Trend(db *gorm.DB) (trend *Trend, err error) {
	trend = NewTrend(db, d)
	if err = trend.Train(); err != nil {
		err = errors.Wrapf(err, "Trend could not be trained for Developer %v", d)
		return
	}

	if _, err = trend.Trend(); err != nil {
		err = errors.Wrapf(err, "Trend could not be run for Developer %v", d)
		return
	}
	return
}

type TrendingDev struct {
	position  *int
	outOf     *int
	Developer *Developer
	Snapshots []*DeveloperSnapshot
	Games     []*Game
	Trend     *Trend
}

func (td *TrendingDev) SetPosition(pos int) { td.position = &pos }
func (td *TrendingDev) GetPosition() int    { return *td.position }
func (td *TrendingDev) HasPosition() bool   { return td.position != nil }

func (td *TrendingDev) SetOutOf(outOf int) { td.outOf = &outOf }
func (td *TrendingDev) GetOutOf() int      { return *td.outOf }
func (td *TrendingDev) HasOutOf() bool     { return td.outOf != nil }

// TrendingDev returns the TrendingDev that is comprised from the Developer's Games, DeveloperSnapshots, and Trend.
func (d *Developer) TrendingDev(db *gorm.DB) (trendingDev *TrendingDev, err error) {
	trendingDev = &TrendingDev{
		Developer: d,
	}

	if trendingDev.Games, err = d.Games(db); err != nil {
		err = errors.Wrapf(err, "could find Games for %v", d)
		return
	}

	if trendingDev.Snapshots, err = d.DeveloperSnapshots(db); err != nil {
		err = errors.Wrapf(err, "could find DeveloperSnapshots for %v", d)
		return
	}

	if trendingDev.Trend, err = d.Trend(db); err != nil {
		err = errors.Wrapf(err, "could not find Trend for %v", d)
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
