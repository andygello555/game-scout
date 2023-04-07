package reddit

import "time"

type RateLimitConfig interface {
	LimitPerMonth() uint64
	LimitPerWeek() uint64
	LimitPerDay() uint64
	LimitPerHour() uint64
	LimitPerMinute() uint64
	LimitPerSecond() uint64
	LimitPerRequest() time.Duration
}

type Config interface {
	RedditPersonalUseScript() string
	RedditSecret() string
	RedditUserAgent() string
	RedditUsername() string
	RedditPassword() string
	RedditSubreddits() []string
	RedditRateLimits() RateLimitConfig
}
