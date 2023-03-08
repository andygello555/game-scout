package reddit

type Config interface {
	RedditPersonalUseScript() string
	RedditSecret() string
	RedditUserAgent() string
	RedditUsername() string
	RedditPassword() string
	RedditSubreddits() []string
}
