package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/game-scout/email"
	task "github.com/andygello555/game-scout/tasks"
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

// DBConfig contains the config variables for the DB to connect to via Gorm.
type DBConfig struct {
	Host     string `json:"host"`
	User     string `json:"user"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Port     int    `json:"port"`
	SSLMode  bool   `json:"sslmode"`
	Timezone string `json:"timezone"`
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
	sslMode := "disable"
	if c.SSLMode {
		sslMode = "enable"
	}
	return sslMode
}
func (c *DBConfig) DBTimezone() string { return c.Timezone }

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

type TaskConfig struct {
	DefaultQueue    string       `json:"default_queue"`
	ResultsExpireIn int          `json:"results_expire_in"`
	Broker          string       `json:"broker"`
	ResultBackend   string       `json:"result_backend"`
	Redis           *RedisConfig `json:"redis"`
}

func (c *TaskConfig) TasksDefaultQueue() string    { return c.DefaultQueue }
func (c *TaskConfig) TasksResultsExpireIn() int    { return c.ResultsExpireIn }
func (c *TaskConfig) TasksBroker() string          { return c.Broker }
func (c *TaskConfig) TasksResultBackend() string   { return c.ResultBackend }
func (c *TaskConfig) TasksRedis() task.RedisConfig { return c.Redis }

type TwitterConfig struct {
	APIKey              string   `json:"api_key"`
	APIKeySecret        string   `json:"api_key_secret"`
	BearerToken         string   `json:"bearer_token"`
	Username            string   `json:"username"`
	Password            string   `json:"password"`
	Hashtags            []string `json:"hashtags"`
	BlacklistedHashtags []string `json:"blacklisted_hashtags"`
	Headless            bool     `json:"headless"`
}

func (c *TwitterConfig) TwitterAPIKey() string       { return c.APIKey }
func (c *TwitterConfig) TwitterAPIKeySecret() string { return c.APIKeySecret }
func (c *TwitterConfig) TwitterBearerToken() string  { return c.BearerToken }
func (c *TwitterConfig) TwitterUsername() string     { return c.Username }
func (c *TwitterConfig) TwitterPassword() string     { return c.Password }
func (c *TwitterConfig) TwitterHashtags() []string   { return c.Hashtags }
func (c *TwitterConfig) TwitterHeadless() bool       { return c.Headless }

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

type StorefrontConfig struct {
	// Storefront is the models.Storefront that this StorefrontConfig applies to.
	Storefront models.Storefront `json:"storefront"`
	// Tags is the TagConfig for the models.Storefront. This only applies to models.SteamStorefront.
	Tags *TagConfig `json:"tags"`
}

func (sfc *StorefrontConfig) StorefrontStorefront() models.Storefront { return sfc.Storefront }
func (sfc *StorefrontConfig) StorefrontTags() models.TagConfig        { return sfc.Tags }

type ScrapeConfig struct {
	// Debug is the value for the State.Debug field.
	Debug bool `json:"debug"`
	// Storefronts is a list of StorefrontConfig that contains the configs for each models.Storefront.
	Storefronts []*StorefrontConfig `json:"storefronts"`
}

func (sc *ScrapeConfig) ScrapeDebug() bool { return sc.Debug }

func (sc *ScrapeConfig) ScrapeStorefronts() []models.StorefrontConfig {
	storefronts := make([]models.StorefrontConfig, len(sc.Storefronts))
	for i, storefront := range sc.Storefronts {
		storefronts[i] = storefront
	}
	return storefronts
}

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

// SteamWebPipesConfig stores the configuration for the SteamWebPipes co-process that's started when the machinery workers
// are started. The SteamWebPipes binary is a slightly modified version of this project: https://github.com/xPaw/SteamWebPipes.
type SteamWebPipesConfig struct {
	BinaryLocation           string `json:"BinaryLocation"`
	Location                 string `json:"Location"`
	DatabaseConnectionString string `json:"DatabaseConnectionString"`
	X509Certificate          string `json:"X509Certificate"`
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
