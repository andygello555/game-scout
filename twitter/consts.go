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
