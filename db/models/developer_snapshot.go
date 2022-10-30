package models

import (
	"fmt"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/volatiletech/null/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"reflect"
	"time"
)

// DeveloperSnapshot is snapshot of a potential developer's public user metrics, aggregations of tweet metrics
type DeveloperSnapshot struct {
	// ID of the snapshot that is automatically generated by postgres.
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4()"`
	// CreatedAt is automatically set to the time that this record is created.
	CreatedAt time.Time
	// Version is the number of this snapshot out of the set of all DeveloperSnapshot for this Developer.
	Version int32
	// DeveloperID is the foreign key to the Developer this snapshot is for.
	DeveloperID string
	Developer   *Developer `gorm:"constraint:OnDelete:CASCADE;"`
	// Tweets is the number of tweets that could be scraped for this Developer for this DeveloperSnapshot.
	Tweets int32
	// TweetTimeRange is the time.Duration between the Developer's earliest tweet (that was scraped) and their latest
	// tweet (that was scraped). This is set to nil when only one tweet was scraped.
	TweetTimeRange NullDuration
	// TweetTimeRange is the average time.Duration between all the Developer's tweets that were scraped for this
	// DeveloperSnapshot. This is set to nil when only one tweet was scraped.
	AverageDurationBetweenTweets NullDuration
	// TweetsPublicMetrics is the sum of all public metrics for the tweets that were scraped for this Developer for this
	// DeveloperSnapshot.
	TweetsPublicMetrics *twitter.TweetMetricsObj `gorm:"embedded;embeddedPrefix:total_tweet_"`
	// UserPublicMetrics is the public metrics for the Developer at the time this DeveloperSnapshot was created.
	UserPublicMetrics *twitter.UserMetricsObj `gorm:"embedded;embeddedPrefix:user_"`
	// ContextAnnotationSet is the set of all ContextAnnotations for all tweets that were scraped for this Developer for
	// this DeveloperSnapshot.
	ContextAnnotationSet *myTwitter.ContextAnnotationSet
	// Games is the number of Game that are currently found for this Developer at the time this DeveloperSnapshot was
	// created. This is a computed field, no need to set it before saving.
	Games int32
	// GameWeightedScoresSum is the sum of all Game.WeightedScore values for all the Game that are currently found for
	// this Developer at the time this DeveloperSnapshot was created. This is a computed field, no need to set it before
	// saving.
	GameWeightedScoresSum float64
	// WeightedScore is a weighted average comprised of the values taken from Tweets, TweetTimeRange,
	// AverageDurationBetweenTweets, TweetsPublicMetrics, UserPublicMetrics, ContextAnnotationSet, Games, and
	// GameWeightedScoresSum. This is a computed field, no need to set it before saving.
	WeightedScore float64
}

// developerSnapshotWeight represents a weight for a developerSnapshotWeightedField. If the developerSnapshotWeight is
// negative then this means to take the inverse of the value first, then multiply it by the
// math.Abs(developerSnapshotWeight).
type developerSnapshotWeight float64

const (
	TweetsWeight                       developerSnapshotWeight = 0.65
	TweetTimeRangeWeight               developerSnapshotWeight = -0.35
	AverageDurationBetweenTweetsWeight developerSnapshotWeight = -0.45
	TweetsPublicMetricsWeight          developerSnapshotWeight = 0.75
	UserPublicMetricsWeight            developerSnapshotWeight = 0.7
	ContextAnnotationSetWeight         developerSnapshotWeight = 0.55
	GamesWeight                        developerSnapshotWeight = -0.2
	GameWeightedScoresSumWeight        developerSnapshotWeight = 0.8
)

// developerSnapshotWeightedField represents a field that can have a weighting calculation applied to it in
// DeveloperSnapshot.
type developerSnapshotWeightedField string

const (
	Tweets                       developerSnapshotWeightedField = "Tweets"
	TweetTimeRange               developerSnapshotWeightedField = "TweetTimeRange"
	AverageDurationBetweenTweets developerSnapshotWeightedField = "AverageDurationBetweenTweets"
	TweetsPublicMetrics          developerSnapshotWeightedField = "TweetsPublicMetrics"
	UserPublicMetrics            developerSnapshotWeightedField = "UserPublicMetrics"
	ContextAnnotationSet         developerSnapshotWeightedField = "ContextAnnotationSet"
	Games                        developerSnapshotWeightedField = "Games"
	GameWeightedScoresSum        developerSnapshotWeightedField = "GameWeightedScoresSum"
)

// String returns the string value of the developerSnapshotWeightedField.
func (wf developerSnapshotWeightedField) String() string { return string(wf) }

// Weight returns the developerSnapshotWeight for a developerSnapshotWeightedField, as well as whether the value should
// have its inverse taken first.
func (wf developerSnapshotWeightedField) Weight() (w float64, inverse bool) {
	switch wf {
	case Tweets:
		w = float64(TweetsWeight)
	case TweetTimeRange:
		w = float64(TweetTimeRangeWeight)
	case AverageDurationBetweenTweets:
		w = float64(AverageDurationBetweenTweetsWeight)
	case TweetsPublicMetrics:
		w = float64(TweetsPublicMetricsWeight)
	case UserPublicMetrics:
		w = float64(UserPublicMetricsWeight)
	case ContextAnnotationSet:
		w = float64(ContextAnnotationSetWeight)
	case Games:
		w = float64(GamesWeight)
	case GameWeightedScoresSum:
		w = float64(GameWeightedScoresSumWeight)
	default:
		panic(fmt.Errorf("\"%s\" is not a developerSnapshotWeightedField", wf))
	}
	inverse = w < 0.0
	w = math.Abs(w)
	return
}

// GetValueFromWeightedModel uses reflection to get the value of the developerSnapshotWeightedField from the given
// DeveloperSnapshot, and will return a list of floats for use in the calculation of the DeveloperSnapshot.WeightedScore.
func (wf developerSnapshotWeightedField) GetValueFromWeightedModel(model WeightedModel) []float64 {
	r := reflect.ValueOf(model)
	f := reflect.Indirect(r).FieldByName(wf.String())
	switch f.Interface().(type) {
	case float64:
		return []float64{f.Float()}
	case int32:
		return []float64{float64(int32(f.Int()))}
	case NullDuration:
		val := 0.0
		duration := f.Interface().(NullDuration)
		if duration.IsValid() {
			val = duration.Ptr().Minutes()
		}
		return []float64{val}
	case *twitter.TweetMetricsObj:
		tweetMetricsObj := f.Interface().(*twitter.TweetMetricsObj)
		return []float64{
			float64(tweetMetricsObj.Impressions),
			float64(tweetMetricsObj.URLLinkClicks),
			float64(tweetMetricsObj.UserProfileClicks),
			float64(tweetMetricsObj.Likes),
			float64(tweetMetricsObj.Replies),
			float64(tweetMetricsObj.Retweets),
			float64(tweetMetricsObj.Quotes),
		}
	case *twitter.UserMetricsObj:
		userMetricsObj := f.Interface().(*twitter.UserMetricsObj)
		return []float64{
			float64(userMetricsObj.Followers),
			float64(userMetricsObj.Following),
			float64(userMetricsObj.Tweets),
			float64(userMetricsObj.Listed),
		}
	case *myTwitter.ContextAnnotationSet:
		contextAnnotationSet := f.Interface().(*myTwitter.ContextAnnotationSet)
		values := make([]float64, contextAnnotationSet.Cardinality())
		iterator := contextAnnotationSet.Set.Iterator()
		var i int
		for contextAnnotation := range iterator.C {
			values[i] = contextAnnotation.Domain.Value()
			i++
		}
		return values
	default:
		panic(fmt.Errorf(
			"developerSnapshotWeightedField has type %s, and cannot be converted to []float64",
			f.Type().String(),
		))
	}
}

func (wf developerSnapshotWeightedField) Fields() []WeightedField {
	return []WeightedField{
		Tweets,
		TweetTimeRange,
		AverageDurationBetweenTweets,
		TweetsPublicMetrics,
		UserPublicMetrics,
		ContextAnnotationSet,
	}
}

// calculateGameField calculates the Games and GameWeightedScoresSum fields for DeveloperSnapshot using the Game table.
func (ds *DeveloperSnapshot) calculateGameField(tx *gorm.DB) (err error) {
	var count int64
	games := tx.Model(&Game{}).Where("developer_id = ?", ds.DeveloperID)
	if games.Error != nil {
		return errors.Wrapf(
			games.Error,
			"could not get Games for developer_id = %s in DeveloperSnapshot.BeforeCreate",
			ds.DeveloperID,
		)
	}
	// Get the number of Games
	if queryCount := games.Count(&count); queryCount.Error == nil {
		ds.Games = int32(count)
	} else if queryCount.Error != nil {
		return errors.Wrapf(
			queryCount.Error,
			"could not count the number of Games for developer_id = %s in DeveloperSnapshot.BeforeCreate",
			ds.DeveloperID,
		)
	}

	// Because Game.WeightedScore can be null, we will filter out any Games with a null weighted_score and also scan the
	// sum of the weighted_scores into a null.Float64 in case there are no games to sum-up.
	gameWeightedScores := null.Float64From(0)
	if err = games.Where("weighted_score IS NOT NULL").Select("sum(weighted_score)").Row().Scan(&gameWeightedScores); err != nil {
		return errors.Wrapf(
			err,
			"could not sum the weighted_scores of Games for developer_id = %s in DeveloperSnapshot.BeforeCreate",
			ds.DeveloperID,
		)
	}
	ds.GameWeightedScoresSum = gameWeightedScores.Float64
	return
}

func (ds *DeveloperSnapshot) UpdateComputedFields(tx *gorm.DB) (err error) {
	ds.WeightedScore = CalculateWeightedScore(ds, Tweets)
	return
}

func (ds *DeveloperSnapshot) Empty() any {
	return &DeveloperSnapshot{}
}

func (ds *DeveloperSnapshot) BeforeCreate(tx *gorm.DB) (err error) {
	// We only calculate the Games and GameWeightedScoresSum when we first create the developer snapshot. This is because
	// we take a snapshot of the games at that current point in time.
	if err = ds.calculateGameField(tx); err != nil {
		return err
	}

	if err = ds.UpdateComputedFields(tx); err != nil {
		return errors.Wrapf(err, "could not update computed fields for DeveloperSnapshot %s", ds.ID.String())
	}

	// Then we calculate the version by looking at the other snapshots for this developer
	developerSnapshots := tx.Model(&DeveloperSnapshot{}).Where("developer_id = ?", ds.DeveloperID)
	if developerSnapshots.Error != nil {
		return errors.Wrapf(
			developerSnapshots.Error,
			"could not get DeveloperSnapshots for developer_id = %s in DeveloperSnapshot.BeforeCreate",
			ds.DeveloperID,
		)
	}
	var count int64
	ds.Version = 0
	if queryCount := developerSnapshots.Count(&count); count > 0 {
		mostRecent := DeveloperSnapshot{}
		if developerSnapshots = developerSnapshots.Order("created_at desc").First(&mostRecent); developerSnapshots.Error != nil {
			return errors.Wrapf(
				developerSnapshots.Error,
				"could not find the most recent DeveloperSnapshot for developer_id = %s in DeveloperSnapshot.BeforeCreate",
				ds.DeveloperID,
			)
		}
		ds.Version = mostRecent.Version + 1
	} else if queryCount.Error != nil {
		return errors.Wrapf(
			queryCount.Error,
			"could not count the number of DeveloperSnapshots for developer_id = %s in DeveloperSnapshot.BeforeCreate",
			ds.DeveloperID,
		)
	}
	return
}

func (ds *DeveloperSnapshot) BeforeUpdate(tx *gorm.DB) (err error) {
	if err = ds.UpdateComputedFields(tx); err != nil {
		err = errors.Wrapf(err, "could not update computed fields for DeveloperSnapshot %s", ds.ID.String())
	}
	return
}

// OnConflict returns the clause.OnConflict that should be checked in an upsert clause.
func (ds *DeveloperSnapshot) OnConflict() clause.OnConflict {
	return clause.OnConflict{Columns: []clause.Column{{Name: "id"}}}
}