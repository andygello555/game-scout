package db

import (
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/volatiletech/null/v9"
	"gorm.io/gorm"
	"time"
)

// Developer represents a potential indie developer's Twitter account. This also contains some current metrics for their
// profile.
type Developer struct {
	ID             string
	Name           string
	Username       string
	Description    string
	ProfileCreated time.Time
	PublicMetrics  *twitter.UserMetricsObj `gorm:"embedded;embeddedPrefix:current_"`
}

// DeveloperSnapshot is snapshot of a potential developer's public user metrics, aggregations of tweet metrics
type DeveloperSnapshot struct {
	ID                           uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4()"`
	CreatedAt                    time.Time
	Version                      int32
	DeveloperID                  string
	Developer                    *Developer `gorm:"constraint:OnDelete:CASCADE;"`
	Tweets                       null.Int32
	TweetTimeRange               NullDuration
	AverageDurationBetweenTweets NullDuration
	TweetsPublicMetrics          *twitter.TweetMetricsObj `gorm:"embedded;embeddedPrefix:total_tweet_"`
	UserPublicMetrics            *twitter.UserMetricsObj  `gorm:"embedded;embeddedPrefix:user_"`
	ContextAnnotationSet         *myTwitter.ContextAnnotationSet
	WeightedScore                float64
}

// calculateWeightedScore calculates the WeightedScore using the available metrics from the DeveloperSnapshot. Each field
// that is used to calculate this is weighted differently.
func (ds *DeveloperSnapshot) calculateWeightedScore() float64 {
	return 0.0
}

func (ds *DeveloperSnapshot) BeforeCreate(tx *gorm.DB) (err error) {
	ds.WeightedScore = ds.calculateWeightedScore()

	// Then we calculate the version by looking at the other snapshots for this developer
	ds.Version = 0
	snapshotsForDeveloper := tx.Where("developer_id = ?", ds.DeveloperID)
	var count int64
	if snapshotsForDeveloper.Count(&count); count > 0 {
		mostRecent := DeveloperSnapshot{}
		snapshotsForDeveloper.Order("created_at desc").First(&mostRecent)
		ds.Version = mostRecent.Version + 1
	}
	return
}

func (ds *DeveloperSnapshot) BeforeUpdate(tx *gorm.DB) (err error) {
	ds.WeightedScore = ds.calculateWeightedScore()
	return
}

// Game represents a game (supposedly) being developed by a Developer.
type Game struct {
	ID          uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4()"`
	Name        string
	Website     string
	DeveloperID string
	Developer   *Developer `gorm:"constraint:OnDelete:CASCADE;"`
}
