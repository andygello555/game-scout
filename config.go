package main

import (
	"encoding/json"
	"github.com/andygello555/game-scout/db/models"
	task "github.com/andygello555/game-scout/tasks"
	"github.com/pkg/errors"
	"os"
	"strings"
)

const ConfigDefaultPath = "config.json"

// DBConfig contains the config variables for the DB to connect to via Gorm.
type DBConfig struct {
	Host     string `json:"host"`
	User     string `json:"user"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Port     int    `json:"port"`
	SSLMode  bool   `json:"sslmode"`
	Timezone string `json:"timezone"`
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
	APIKey       string   `json:"api_key"`
	APIKeySecret string   `json:"api_key_secret"`
	BearerToken  string   `json:"bearer_token"`
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	Hashtags     []string `json:"hashtags"`
}

func (c *TwitterConfig) TwitterAPIKey() string       { return c.APIKey }
func (c *TwitterConfig) TwitterAPIKeySecret() string { return c.APIKeySecret }
func (c *TwitterConfig) TwitterBearerToken() string  { return c.BearerToken }
func (c *TwitterConfig) TwitterUsername() string     { return c.Username }
func (c *TwitterConfig) TwitterPassword() string     { return c.Password }
func (c *TwitterConfig) TwitterHashtags() []string   { return c.Hashtags }

// TwitterQuery constructs a query from the Hashtags array by first prefixing each hashtag with a hash ("#"), then joining
// the list with the " OR " separator.
func (c *TwitterConfig) TwitterQuery() string {
	hashtags := make([]string, len(c.Hashtags))
	for i, hashtag := range c.Hashtags {
		hashtags[i] = "#" + hashtag
	}
	return strings.Join(hashtags, " OR ")
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
	// Storefronts is a list of StorefrontConfig that contains the configs for each models.Storefront.
	Storefronts []*StorefrontConfig `json:"storefronts"`
}

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

// Config contains the sub-configs for the various parts of the game-scout system. Such as the DBConfig.
type Config struct {
	DB      *DBConfig      `json:"db"`
	Tasks   *TaskConfig    `json:"tasks"`
	Twitter *TwitterConfig `json:"twitter"`
	Scrape  *ScrapeConfig  `json:"scrape"`
}

var globalConfig *Config

// LoadConfig loads the config.json file from the root of the repository into the globalConfig variable.
func LoadConfig() error {
	var configData []byte
	var err error
	if configData, err = os.ReadFile(ConfigDefaultPath); err != nil {
		return err
	}

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
