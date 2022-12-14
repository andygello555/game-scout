package twitter

import (
	"time"
)

type RateLimit uint64

const (
	TweetsPerMonth  RateLimit = 1650000
	TweetsPerWeek   RateLimit = 385000
	TweetsPerDay    RateLimit = 55000
	TweetsPerHour   RateLimit = 2200
	TweetsPerMinute RateLimit = 1000
	TweetsPerSecond RateLimit = 200
)

const (
	TimePerRequest          = time.Millisecond * 500
	DefaultTweetCapLocation = "tweetCap.json"
	CreatedAtFormat         = "2006-01-02T15:04:05.000Z"
	// IgnoredErrorTypes is a comma-seperated list of Twitter API error types
	// (https://developer.twitter.com/en/support/twitter-api/error-troubleshooting) that we can safely ignore.
	IgnoredErrorTypes = "https://api.twitter.com/2/problems/resource-not-found;https://api.twitter.com/2/problems/not-authorized-for-resource"
)

// GetRateLimitFromDuration will return the upper RateLimit bound when given a time.Duration.
func GetRateLimitFromDuration(duration time.Duration) RateLimit {
	switch {
	case time.Hour*168 < duration:
		return TweetsPerMonth
	case time.Hour*24 < duration && duration <= time.Hour*168:
		return TweetsPerWeek
	case time.Hour < duration && duration <= time.Hour*24:
		return TweetsPerDay
	case time.Minute < duration && duration <= time.Hour:
		return TweetsPerHour
	case time.Second < duration && duration <= time.Minute:
		return TweetsPerMinute
	default:
		return TweetsPerSecond
	}
}
