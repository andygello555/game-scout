package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/game-scout/email"
	task "github.com/andygello555/game-scout/tasks"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/pkg/errors"
	"jaytaylor.com/html2text"
	"os"
	"strings"
	"time"
)

const ConfigDefaultPath = "config.json"

// PhaseRWAccessConfig represents the read/write permissions for a specific Phase.
type PhaseRWAccessConfig struct {
	Phase Phase `json:"phase"`
	Read  bool  `json:"read"`
	Write bool  `json:"write"`
}

func (rw *PhaseRWAccessConfig) ID() int { return int(rw.Phase) }
func (rw *PhaseRWAccessConfig) R() bool { return rw.Read }
func (rw *PhaseRWAccessConfig) W() bool { return rw.Write }

func (rw *PhaseRWAccessConfig) String() string {
	return fmt.Sprintf("{Phase: %v, Read: %v, Write: %v}", rw.Phase, rw.Read, rw.Write)
}

// DBConfig contains the config variables for the DB to connect to via Gorm.
type DBConfig struct {
	Host     string `json:"host"`
	User     string `json:"user"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Port     int    `json:"port"`
	SSLMode  bool   `json:"sslmode"`
	Timezone string `json:"timezone"`
	// PostgresDBName is the name of the main PostgreSQL database. This is used so that we can create and drop the test
	// database when running tests.
	PostgresDBName string `json:"postgres_db_name"`
	// PhaseRWAccess this is the DB read/write access for individual phases.
	// TODO: Implement a way of asserting read/write access to the DB in different phases, probably by creating a custom
	//       clause or by creating a set of callbacks?
	PhaseRWAccess []*PhaseRWAccessConfig `json:"phase_rw_access"`
	// DefaultPhaseRWAccess describes the default PhaseRWAccess that will be used when a given Phase does not exist in
	// PhaseRWAccess.
	DefaultPhaseRWAccess *PhaseRWAccessConfig `json:"default_phase_rw_access"`
}

func (c *DBConfig) DBHost() string     { return c.Host }
func (c *DBConfig) DBUser() string     { return c.User }
func (c *DBConfig) DBPassword() string { return c.Password }
func (c *DBConfig) DBName() string     { return c.Name }
func (c *DBConfig) TestDBName() string { return "test_" + c.DBName() }
func (c *DBConfig) DBPort() int        { return c.Port }
func (c *DBConfig) DBSSLMode() string {
	return map[bool]string{true: "enable", false: "disable"}[c.SSLMode]
}
func (c *DBConfig) DBTimezone() string       { return c.Timezone }
func (c *DBConfig) DBPostgresDBName() string { return c.PostgresDBName }

func (c *DBConfig) DBDefaultRWAccess() (config db.RWConfig) {
	return c.DefaultPhaseRWAccess
}

func (c *DBConfig) DBRWAccessConfigForID(id int) (config db.RWConfig) {
	for _, config = range c.PhaseRWAccess {
		if config.ID() == id {
			break
		}
	}
	if config == nil {
		config = c.DefaultPhaseRWAccess
	}
	return
}

func (c *DBConfig) DBRWAccessForID(id int) (read bool, write bool) {
	phaseConfig := c.DBRWAccessConfigForID(id)
	return phaseConfig.R(), phaseConfig.R()
}

func (c *DBConfig) DBPhaseReadAccess(id int) bool {
	read, _ := c.DBRWAccessForID(id)
	return read
}

func (c *DBConfig) DBPhaseWriteAccess(id int) bool {
	_, write := c.DBRWAccessForID(id)
	return write
}

func (c *DBConfig) String() string {
	return fmt.Sprintf(
		"{Host: %v, User: %v, Password: %v, Name: %v, Port: %v, SSLMode: %v, Timezone: %v, PhaseRWAccess: %v, DefaultPhaseRWAccess: %v}",
		c.Host, c.User, c.Password, c.Name, c.Port, c.SSLMode, c.Timezone, c.PhaseRWAccess, c.DefaultPhaseRWAccess,
	)
}

// TemplateConfig contains the constants used for a specific email.TemplatePath.
type TemplateConfig struct {
	// MaxImageWidth is the maximum width of any converted base64 images that are going to be displayed in the
	// email.Template.
	MaxImageWidth int `json:"max_image_width"`
	// MaxImageWidth is the maximum height of any converted base64 images that are going to be displayed in the
	// email.Template.
	MaxImageHeight int `json:"max_image_height"`
	// DebugTo is the recipients that this email.Template will be sent to when the ScoutState debug flag is set.
	DebugTo []string `json:"debug_to"`
	// To is the recipients that this email.Template will be sent to when the ScoutState debug flag is NOT set.
	To []string `json:"to"`
	// SubjectFormat is the time format of the subject of the emails sent for this email.Template.
	SubjectFormat string `json:"subject_format"`
	// AttachmentNameFormat is the time format of the filename of the file that will be attached to emails sent for this
	// email.Template. Note: this should not be suffixed with the file extension as this will be done automatically.
	AttachmentNameFormat string `json:"attachment_name_format"`
	// SendRetries is the number of times the email.ClientWrapper.SendAsync method should be retried. If it is negative then
	// it will be retried forever.
	SendRetries int `json:"send_retries"`
	// SendBackoff is the duration that will be multiplied by the current retry number to produce a wait time that will
	// be slept for when an error occurs whilst sending this email.Template.
	SendBackoff string `json:"send_backoff"`
	// HTML2TextOptions are the html2text.Options used when sending instances of this email.Template in an email.
	HTML2TextOptions html2text.Options `json:"html2text_options"`
	// PlainOnly indicates whether to only send the plain-text when sending instances of this email.Template in an
	// email. If it is not set, then the HTML content will also be added.
	PlainOnly bool `json:"plain_only"`
	// SendDay is the time.Weekday that instances of this email.Template are sent. This is ignored if the debug flag is
	// set in ScoutState.
	SendDay time.Weekday `json:"send_day"`
}

func (c *TemplateConfig) TemplateMaxImageWidth() int  { return c.MaxImageWidth }
func (c *TemplateConfig) TemplateMaxImageHeight() int { return c.MaxImageHeight }
func (c *TemplateConfig) TemplateDebugTo() []string   { return c.DebugTo }
func (c *TemplateConfig) TemplateTo() []string        { return c.To }
func (c *TemplateConfig) TemplateSubject() string {
	return time.Now().Format(c.SubjectFormat)
}
func (c *TemplateConfig) TemplateAttachmentName() string {
	return time.Now().Format(c.AttachmentNameFormat)
}
func (c *TemplateConfig) TemplateSendRetries() int { return c.SendRetries }
func (c *TemplateConfig) TemplateSendBackoff() (time.Duration, error) {
	return time.ParseDuration(c.SendBackoff)
}
func (c *TemplateConfig) TemplateHTML2TextOptions() html2text.Options { return c.HTML2TextOptions }
func (c *TemplateConfig) TemplatePlainOnly() bool                     { return c.PlainOnly }
func (c *TemplateConfig) TemplateSendDay() time.Weekday               { return c.SendDay }

func (c *TemplateConfig) String() string {
	return fmt.Sprintf(
		"{MaxImageWidth: %v, MaxImageHeight: %v, DebugTo: %v, To: %v, SubjectFormat: %v, AttachmentNameFormat: %v, SendRetries: %v, SendBackoff: %v, HTML2TextOptions: %v, PlainOnly: %v, SendDay: %v}",
		c.MaxImageWidth, c.MaxImageHeight, c.DebugTo, c.To, c.SubjectFormat, c.AttachmentNameFormat, c.SendRetries, c.SendBackoff, c.HTML2TextOptions, c.PlainOnly, c.SendDay,
	)
}

// EmailConfig contains the variables we need to create our SMTP email client.
type EmailConfig struct {
	Debug           bool                                   `json:"debug"`
	Host            string                                 `json:"host"`
	Port            int                                    `json:"port"`
	From            string                                 `json:"from"`
	FromName        string                                 `json:"from_name"`
	Password        string                                 `json:"password"`
	TemplateConfigs map[email.TemplatePath]*TemplateConfig `json:"template_configs"`
}

func (c *EmailConfig) EmailDebug() bool      { return c.Debug }
func (c *EmailConfig) EmailHost() string     { return c.Host }
func (c *EmailConfig) EmailPort() int        { return c.Port }
func (c *EmailConfig) EmailAddress() string  { return fmt.Sprintf("%s:%d", c.Host, c.Port) }
func (c *EmailConfig) EmailFrom() string     { return c.From }
func (c *EmailConfig) EmailFromName() string { return c.FromName }
func (c *EmailConfig) EmailPassword() string { return c.Password }
func (c *EmailConfig) EmailTemplateConfigFor(path email.TemplatePath) email.TemplateConfig {
	return c.TemplateConfigs[path]
}

func (c *EmailConfig) String() string {
	return fmt.Sprintf(
		"{Debug: %v, Host: %v, Port: %v, From: %v, FromName: %v, Password: %v, TemplateConfigs: %v}",
		c.Debug, c.Host, c.Port, c.From, c.FromName, c.Password, c.TemplateConfigs,
	)
}

type RedisConfig struct {
	MaxIdle                int `json:"max_idle"`
	IdleTimeout            int `json:"idle_timeout"`
	ReadTimeout            int `json:"read_timeout"`
	WriteTimeout           int `json:"write_timeout"`
	ConnectTimeout         int `json:"connect_timeout"`
	NormalTasksPollPeriod  int `json:"normal_tasks_poll_period"`
	DelayedTasksPollPeriod int `json:"delayed_tasks_poll_period"`
}

func (c *RedisConfig) RedisMaxIdle() int                { return c.MaxIdle }
func (c *RedisConfig) RedisIdleTimeout() int            { return c.IdleTimeout }
func (c *RedisConfig) RedisReadTimeout() int            { return c.ReadTimeout }
func (c *RedisConfig) RedisWriteTimeout() int           { return c.WriteTimeout }
func (c *RedisConfig) RedisConnectTimeout() int         { return c.ConnectTimeout }
func (c *RedisConfig) RedisNormalTasksPollPeriod() int  { return c.NormalTasksPollPeriod }
func (c *RedisConfig) RedisDelayedTasksPollPeriod() int { return c.DelayedTasksPollPeriod }

func (c *RedisConfig) String() string {
	return fmt.Sprintf(
		"{MaxIdle: %v, IdleTimeout: %v, ReadTimeout: %v, WriteTimeout: %v, ConnectTimeout: %v, NormalTasksPollPeriod: %v, DelayedTasksPollPeriod: %v}",
		c.MaxIdle, c.IdleTimeout, c.ReadTimeout, c.WriteTimeout, c.ConnectTimeout, c.NormalTasksPollPeriod, c.DelayedTasksPollPeriod,
	)
}

type PeriodicTaskSignature struct {
	Args       []tasks.Arg `json:"args"`
	Cron       string      `json:"cron"`
	RetryCount int         `json:"retry_count"`
}

func (pts PeriodicTaskSignature) PeriodicTaskSignatureArgs() []tasks.Arg { return pts.Args }
func (pts PeriodicTaskSignature) PeriodicTaskSignatureCron() string      { return pts.Cron }
func (pts PeriodicTaskSignature) PeriodicTaskSignatureRetryCount() int   { return pts.RetryCount }

func (pts PeriodicTaskSignature) String() string {
	return fmt.Sprintf("{Args: %v, Cron: %v, RetryCount: %v}", pts.Args, pts.Cron, pts.RetryCount)
}

type TaskConfig struct {
	DefaultQueue           string                           `json:"default_queue"`
	ResultsExpireIn        int                              `json:"results_expire_in"`
	Broker                 string                           `json:"broker"`
	ResultBackend          string                           `json:"result_backend"`
	Redis                  *RedisConfig                     `json:"redis"`
	PeriodicTaskSignatures map[string]PeriodicTaskSignature `json:"periodic_task_signatures"`
}

func (c *TaskConfig) TasksDefaultQueue() string    { return c.DefaultQueue }
func (c *TaskConfig) TasksResultsExpireIn() int    { return c.ResultsExpireIn }
func (c *TaskConfig) TasksBroker() string          { return c.Broker }
func (c *TaskConfig) TasksResultBackend() string   { return c.ResultBackend }
func (c *TaskConfig) TasksRedis() task.RedisConfig { return c.Redis }
func (c *TaskConfig) TasksPeriodicTaskSignatures() map[string]task.PeriodicTaskSignature {
	periodicTaskSignatures := make(map[string]task.PeriodicTaskSignature)
	for name, sig := range c.PeriodicTaskSignatures {
		periodicTaskSignatures[name] = sig
	}
	return periodicTaskSignatures
}
func (c *TaskConfig) TasksPeriodicTaskSignature(taskName string) task.PeriodicTaskSignature {
	return c.PeriodicTaskSignatures[taskName]
}

func (c *TaskConfig) String() string {
	return fmt.Sprintf(
		"{DefaultQueue: %v, ResultsExpireIn: %v, Broker: %v, ResultBackend: %v, Redis: %v, PeriodicTaskSignatures: %v}",
		c.DefaultQueue, c.ResultsExpireIn, c.Broker, c.ResultBackend, c.Redis, c.PeriodicTaskSignatures,
	)
}

type TwitterRateLimits struct {
	TweetsPerMonth  uint64   `json:"tweets_per_month"`
	TweetsPerWeek   uint64   `json:"tweets_per_week"`
	TweetsPerDay    uint64   `json:"tweets_per_day"`
	TweetsPerHour   uint64   `json:"tweets_per_hour"`
	TweetsPerMinute uint64   `json:"tweets_per_minute"`
	TweetsPerSecond uint64   `json:"tweets_per_second"`
	TimePerRequest  Duration `json:"time_per_request"`
}

func (rl *TwitterRateLimits) LimitPerMonth() uint64          { return rl.TweetsPerMonth }
func (rl *TwitterRateLimits) LimitPerWeek() uint64           { return rl.TweetsPerWeek }
func (rl *TwitterRateLimits) LimitPerDay() uint64            { return rl.TweetsPerDay }
func (rl *TwitterRateLimits) LimitPerHour() uint64           { return rl.TweetsPerHour }
func (rl *TwitterRateLimits) LimitPerMinute() uint64         { return rl.TweetsPerMinute }
func (rl *TwitterRateLimits) LimitPerSecond() uint64         { return rl.TweetsPerSecond }
func (rl *TwitterRateLimits) LimitPerRequest() time.Duration { return rl.TimePerRequest.Duration }

func (rl *TwitterRateLimits) String() string {
	return fmt.Sprintf(
		"{TweetsPerMonth: %v, TweetsPerWeek: %v, TweetsPerDay: %v, TweetsPerHour: %v, TweetsPerMinute: %v, TweetsPerSecond: %v, TimePerRequest: %v}",
		rl.TweetsPerMonth, rl.TweetsPerWeek, rl.TweetsPerDay, rl.TweetsPerHour, rl.TweetsPerMinute, rl.TweetsPerSecond, rl.TimePerRequest,
	)
}

type TwitterConfig struct {
	APIKey              string             `json:"api_key"`
	APIKeySecret        string             `json:"api_key_secret"`
	BearerToken         string             `json:"bearer_token"`
	Username            string             `json:"username"`
	Password            string             `json:"password"`
	Hashtags            []string           `json:"hashtags"`
	BlacklistedHashtags []string           `json:"blacklisted_hashtags"`
	Headless            bool               `json:"headless"`
	RateLimits          *TwitterRateLimits `json:"rate_limits"`
	TweetCapLocation    string             `json:"tweet_cap_location"`
	CreatedAtFormat     string             `json:"created_at_format"`
	// IgnoredErrorTypes is a semi-colon-seperated list of Twitter API error types
	// (https://developer.twitter.com/en/support/twitter-api/error-troubleshooting) that we can safely ignore.
	IgnoredErrorTypes []string `json:"ignored_error_types"`
}

func (c *TwitterConfig) TwitterAPIKey() string                   { return c.APIKey }
func (c *TwitterConfig) TwitterAPIKeySecret() string             { return c.APIKeySecret }
func (c *TwitterConfig) TwitterBearerToken() string              { return c.BearerToken }
func (c *TwitterConfig) TwitterUsername() string                 { return c.Username }
func (c *TwitterConfig) TwitterPassword() string                 { return c.Password }
func (c *TwitterConfig) TwitterHashtags() []string               { return c.Hashtags }
func (c *TwitterConfig) TwitterHeadless() bool                   { return c.Headless }
func (c *TwitterConfig) TwitterRateLimits() myTwitter.RateLimits { return c.RateLimits }
func (c *TwitterConfig) TwitterTweetCapLocation() string         { return c.TweetCapLocation }
func (c *TwitterConfig) TwitterCreatedAtFormat() string          { return c.CreatedAtFormat }
func (c *TwitterConfig) TwitterIgnoredErrorTypes() []string      { return c.IgnoredErrorTypes }

func (c *TwitterConfig) String() string {
	return fmt.Sprintf(
		"{APIKey: %v, APIKeySecret: %v, BearerToken: %v, Username: %v, Password: %v, Hashtags: %v, BlacklistedHashtags: %v, Headless: %v, RateLimits: %v}",
		c.APIKey, c.APIKeySecret, c.BearerToken, c.Username, c.Password, c.Hashtags, c.BlacklistedHashtags, c.Headless, c.RateLimits,
	)
}

// TwitterQuery constructs a query from the Hashtags array and the BlacklistedHashtags array by first prefixing each
// hashtag with a hash ("#") and each blacklisted hashtag with "-#", then joining them with the " OR " separator.
func (c *TwitterConfig) TwitterQuery() string {
	hashtags := make([]string, len(c.Hashtags))
	for i, hashtag := range c.Hashtags {
		hashtags[i] = "#" + hashtag
	}
	query := strings.Join(hashtags, " OR ")

	hashtags = make([]string, len(c.BlacklistedHashtags))
	for i, blacklistedHashtag := range c.BlacklistedHashtags {
		hashtags[i] = "-#" + blacklistedHashtag
	}
	return fmt.Sprintf("%s (%s)", query, strings.Join(hashtags, " OR "))
}

type TagConfig struct {
	// DefaultValue is the default value for tags that are not included in the Values map. The value of each tag for a
	// models.Game (on the models.SteamStorefront) will be multiplied by the number of upvotes it has then accumulated.
	// The average of this accumulated value will be used to calculate models.Game.WeightedScore.
	DefaultValue float64 `json:"default_value"`
	// UpvotesThreshold is the threshold for the number of upvotes a tag should have (i.e. >=) to be included in the
	// accumulated value of all the tags for a models.Game. If this is not set (== 0), then all the tags will be added.
	UpvotesThreshold float64 `json:"upvotes_threshold"`
	// Values is a map of tag names to values. It is useful for soft "banning" tags that we don't want to see in a game.
	Values map[string]float64 `json:"values"`
}

func (tc *TagConfig) TagDefaultValue() float64      { return tc.DefaultValue }
func (tc *TagConfig) TagUpvotesThreshold() float64  { return tc.UpvotesThreshold }
func (tc *TagConfig) TagValues() map[string]float64 { return tc.Values }

func (tc *TagConfig) String() string {
	return fmt.Sprintf(
		"{DefaultValue: %v, UpvotesThreshold: %v, Values: %v}",
		tc.DefaultValue, tc.UpvotesThreshold, tc.Values,
	)
}

type StorefrontConfig struct {
	// Storefront is the models.Storefront that this StorefrontConfig applies to.
	Storefront models.Storefront `json:"storefront"`
	// Tags is the TagConfig for the models.Storefront. This only applies to models.SteamStorefront.
	Tags *TagConfig `json:"tags"`
}

func (sfc *StorefrontConfig) StorefrontStorefront() models.Storefront { return sfc.Storefront }
func (sfc *StorefrontConfig) StorefrontTags() models.TagConfig        { return sfc.Tags }

func (sfc *StorefrontConfig) String() string {
	return fmt.Sprintf("{Storefront: %v, Tags: %v}", sfc.Storefront, sfc.Tags)
}

// Duration that can JSON serialised/deserialised. To be used in configs.
type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value)
		return nil
	case string:
		var err error
		d.Duration, err = time.ParseDuration(value)
		if err != nil {
			return err
		}
		return nil
	default:
		return errors.New("invalid duration")
	}
}

type ScrapeConstants struct {
	// TransformTweetWorkers is the number of transformTweetWorker that will be spun up in the DiscoveryBatch.
	TransformTweetWorkers int `json:"transform_tweet_workers"`
	// UpdateDeveloperWorkers is the number of updateDeveloperWorker that will be spun up in the update phase.
	UpdateDeveloperWorkers int `json:"update_developer_workers"`
	// MaxUpdateTweets is the maximum number of tweets fetched in the update phase.
	MaxUpdateTweets int `json:"max_update_tweets"`
	// SecondsBetweenDiscoveryBatches is the number of seconds to sleep between DiscoveryBatch batches.
	SecondsBetweenDiscoveryBatches Duration `json:"seconds_between_discovery_batches"`
	// SecondsBetweenUpdateBatches is the number of seconds to sleep between queue batches of updateDeveloperJob.
	SecondsBetweenUpdateBatches Duration `json:"seconds_between_update_batches"`
	// MaxTotalDiscoveryTweetsDailyPercent is the maximum percentage that the discoveryTweets number can be out of
	// myTwitter.TweetsPerDay.
	MaxTotalDiscoveryTweetsDailyPercent float64 `json:"max_total_discovery_tweets_daily_percent"`
	// MaxEnabledDevelopersAfterEnablePhase is the number of enabled developers that should exist after the Enable phase.
	MaxEnabledDevelopersAfterEnablePhase float64 `json:"max_enabled_developers_after_enable_phase"`
	// MaxEnabledDevelopersAfterDisablePhase is the number of developers to keep in the Disable phase.
	MaxEnabledDevelopersAfterDisablePhase float64 `json:"max_enabled_developers_after_disable_phase"`
	// MaxDevelopersToEnable is the maximum number of developers that can be re-enabled in the Enable phase.
	MaxDevelopersToEnable float64 `json:"max_developers_to_enable"`
	// PercentageOfDisabledDevelopersToDelete is the percentage of all disabled developers to delete in the Delete phase.
	PercentageOfDisabledDevelopersToDelete float64 `json:"percentage_of_disabled_developers_to_delete"`
	// StaleDeveloperDays is the number of days after which a developer can become stale if their latest snapshot was
	// created StaleDeveloperDays ago.
	StaleDeveloperDays int `json:"stale_developer_days"`
	// DiscoveryGameScrapeWorkers is the number of models.StorefrontScrapers to start in the Discovery Phase.
	DiscoveryGameScrapeWorkers int `json:"discovery_game_scrape_workers"`
	// DiscoveryMaxConcurrentGameScrapeWorkers is the number of models.StorefrontScrapers that can be processing a job
	// at the same time in the Discovery Phase.
	DiscoveryMaxConcurrentGameScrapeWorkers int `json:"discovery_max_concurrent_game_scrape_workers"`
	// UpdateGameScrapeWorkers is the number of models.StorefrontScrapers to start in the Update Phase.
	UpdateGameScrapeWorkers int `json:"update_game_scrape_workers"`
	// UpdateMaxConcurrentGameScrapeWorkers is number of models.StorefrontScrapers that can be processing a job at the
	// same time in the Update Phase.
	UpdateMaxConcurrentGameScrapeWorkers int `json:"update_max_concurrent_game_scrape_workers"`

	// MinScrapeStorefrontsForGameWorkerWaitTime is the minimum amount of time for a models.StorefrontScrapers to wait
	// after completing a job in the Discovery and Update Phase.
	MinScrapeStorefrontsForGameWorkerWaitTime Duration `json:"min_scrape_storefronts_for_game_worker_wait_time"`
	// MaxScrapeStorefrontsForGameWorkerWaitTime is the maximum amount of time for a models.StorefrontScrapers to wait
	// after completing a job in the Discovery and Update Phase.
	MaxScrapeStorefrontsForGameWorkerWaitTime Duration `json:"max_scrape_storefronts_for_game_worker_wait_time"`
	// MaxGamesPerTweet is the maximum number of games for each tweet we process in the Discovery and Update Phase. This
	// is needed so that we don't overload the queue to the models.StorefrontScrapers.
	MaxGamesPerTweet int `json:"max_games_per_tweet"`

	// MaxTrendWorkers is the maximum number of trendFinder workers to start to find the models.Trend,
	// models.DeveloperSnapshot, and models.Game for a models.Developer to use in the models.TrendingDev field in
	// email.MeasureContext for the email.Measure email email.Template.
	MaxTrendWorkers int `json:"max_trend_workers"`
	// MaxTrendingDevelopers is the maximum number of models.TrendingDev to feature within the email.Measure email
	// email.Template.
	MaxTrendingDevelopers int `json:"max_trending_developers"`
	// MaxTopSteamApps is the maximum number of models.SteamApp to feature within the email.Measure email
	// email.Template.
	MaxTopSteamApps int `json:"max_top_steam_apps"`

	// SockSteamAppScrapeWorkers is the number of models.StorefrontScrapers to start in the goroutine that will watch
	// the output of the ScoutWebPipes co-process.
	SockSteamAppScrapeWorkers int `json:"sock_steam_app_scrape_workers"`
	// SockMaxConcurrentSteamAppScrapeWorkers is the number of models.StorefrontScrapers that can be processing a job at
	// the same time in the websocket client.
	SockMaxConcurrentSteamAppScrapeWorkers int `json:"sock_max_concurrent_steam_app_scrape_workers"`
	// SockMaxSteamAppScraperJobs is the maximum number of jobs that can be queued for the models.StorefrontScrapers
	// instance in the websocket client.
	SockMaxSteamAppScraperJobs int `json:"sock_max_steam_app_scraper_jobs"`
	// SockSteamAppScraperJobsPerMinute is the number of models.SteamApp the websocket client should be completing every
	// minute. If it falls below this threshold, and the number of jobs in the models.StorefrontScrapers queue is above
	// SockSteamAppScraperDropJobsThreshold then the dropper goroutine will drop some jobs the next time it is running.
	SockSteamAppScraperJobsPerMinute int `json:"sock_steam_app_scraper_jobs_per_minute"`
	// SockSteamAppScraperDropJobsThreshold is the threshold of jobs in the models.StorefrontScrapers at which the
	// dropper goroutine will start to drop jobs to make room.
	SockSteamAppScraperDropJobsThreshold int `json:"sock_steam_app_scraper_drop_jobs_threshold"`
	// SockMinSteamAppScraperWaitTime is the minimum amount of time for a models.StorefrontScrapers to wait after
	// completing a job in the websocket client.
	SockMinSteamAppScraperWaitTime Duration `json:"sock_min_steam_app_scraper_wait_time"`
	// SockMaxSteamAppScraperWaitTime is the maximum amount of time for a models.StorefrontScrapers to wait after
	// completing a job in the websocket client.
	SockMaxSteamAppScraperWaitTime Duration `json:"sock_max_steam_app_scraper_wait_time"`
	// SockSteamAppScrapeConsumers is the number of consumers to start in the websocket client to consume the scraped
	// models.SteamApp from the models.StorefrontScrapers instance.
	SockSteamAppScrapeConsumers int `json:"sock_steam_app_scrape_consumers"`
	// SockDropperWaitTime is the amount time for the dropper goroutine to wait each time it has been run.
	SockDropperWaitTime Duration `json:"sock_dropper_wait_time"`

	// ScrapeMaxTries is the maximum number of tries that is passed to a models.GameModelStorefrontScraper from
	// models.ScrapeStorefrontForGameModel in a models.StorefrontScrapers instance. This affects the number of times a
	// certain scrape procedure will be retried for a certain models.Storefront.
	ScrapeMaxTries int `json:"scrape_max_tries"`
	// ScrapeMinDelay is the minimum wait time after an unsuccessful try that is passed to a
	// models.GameModelStorefrontScraper from models.ScrapeStorefrontForGameModel in a models.StorefrontScrapers
	// instance. This affects the wait time after an unsuccessful call to a scrape procedure for a certain models.Storefront.
	ScrapeMinDelay Duration `json:"scrape_min_delay"`
}

// maxTotalDiscoveryTweets is the maximum number of discoveryTweets that can be given to Scout.
func (c *ScrapeConstants) maxTotalDiscoveryTweets() float64 {
	return float64(globalConfig.Twitter.RateLimits.TweetsPerDay) * c.MaxTotalDiscoveryTweetsDailyPercent
}

// maxTotalUpdateTweets is the maximum number of tweets that can be scraped by the Update phase.
func (c *ScrapeConstants) maxTotalUpdateTweets() float64 {
	return float64(globalConfig.Twitter.RateLimits.TweetsPerDay) * (1.0 - c.MaxTotalDiscoveryTweetsDailyPercent)
}

func (c *ScrapeConstants) DefaultMaxTries() int           { return c.ScrapeMaxTries }
func (c *ScrapeConstants) DefaultMinDelay() time.Duration { return c.ScrapeMinDelay.Duration }

type ScrapeConfig struct {
	// Debug is the value for the State.Debug field.
	Debug bool `json:"debug"`
	// Storefronts is a list of StorefrontConfig that contains the configs for each models.Storefront.
	Storefronts []*StorefrontConfig `json:"storefronts"`
	// Constants are all the constants used throughout the Scout procedure as well as the ScoutWebPipes co-process.
	Constants *ScrapeConstants
}

func (sc *ScrapeConfig) ScrapeDebug() bool { return sc.Debug }

func (sc *ScrapeConfig) ScrapeStorefronts() []models.StorefrontConfig {
	storefronts := make([]models.StorefrontConfig, len(sc.Storefronts))
	for i, storefront := range sc.Storefronts {
		storefronts[i] = storefront
	}
	return storefronts
}

func (sc *ScrapeConfig) ScrapeConstants() models.ScrapeConstants { return sc.Constants }

func (sc *ScrapeConfig) ScrapeGetStorefront(storefront models.Storefront) (storefrontConfig models.StorefrontConfig) {
	found := false
	for _, storefrontConfig = range sc.Storefronts {
		if storefrontConfig.StorefrontStorefront() == storefront {
			found = true
			break
		}
	}
	if !found {
		storefrontConfig = nil
	}
	return
}

func (sc *ScrapeConfig) String() string {
	return fmt.Sprintf("{Debug: %v, Storefronts: %v, Constants: %v}", sc.Debug, sc.Storefronts, sc.Constants)
}

// SteamWebPipesConfig stores the configuration for the SteamWebPipes co-process that's started when the machinery workers
// are started. The SteamWebPipes binary is a slightly modified version of this project: https://github.com/xPaw/SteamWebPipes.
type SteamWebPipesConfig struct {
	BinaryLocation           string `json:"BinaryLocation"`
	Location                 string `json:"Location"`
	DatabaseConnectionString string `json:"DatabaseConnectionString"`
	X509Certificate          string `json:"X509Certificate"`
}

func (c *SteamWebPipesConfig) String() string {
	return fmt.Sprintf(
		"{BinaryLocation: %v, Location: %v, DatabaseConnectionString: %v, X509Certificate: %v}",
		c.BinaryLocation, c.Location, c.DatabaseConnectionString, c.X509Certificate,
	)
}

// Config contains the sub-configs for the various parts of the game-scout system. Such as the DBConfig.
type Config struct {
	DB            *DBConfig            `json:"db"`
	Email         *EmailConfig         `json:"email"`
	Tasks         *TaskConfig          `json:"tasks"`
	Twitter       *TwitterConfig       `json:"twitter"`
	Scrape        *ScrapeConfig        `json:"scrape"`
	SteamWebPipes *SteamWebPipesConfig `json:"SteamWebPipes"`
}

var globalConfig *Config

// LoadConfig loads the config.json file from the root of the repository into the globalConfig variable.
func LoadConfig() error {
	var configData []byte
	var err error
	if configData, err = os.ReadFile(ConfigDefaultPath); err != nil {
		return err
	}

	// Remove BOM, if there is one
	configData = bytes.TrimPrefix(configData, []byte("\xef\xbb\xbf"))

	globalConfig = &Config{}
	if err = json.Unmarshal(configData, globalConfig); err != nil {
		return err
	}
	return nil
}

// ToJSON converts the Config back to JSON.
func (c *Config) ToJSON() (jsonData []byte, err error) {
	if jsonData, err = json.Marshal(c); err != nil {
		return jsonData, errors.Wrap(err, "could not Marshal Config to JSON")
	}
	return
}
