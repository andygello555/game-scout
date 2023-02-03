# game-scout

A tool that tracks indie developers on Twitter and highlights up and coming developers

## Configuration

All configuration variables for game-scout exist in a `config.json` file that must be in the same working directory as the game-scout executable. This is usually the root of the repository. The following is the default `config.json` for game-scout:

```json
{
  "db": {
    "host": "<DB_HOST>",
    "user": "<DB_USER>",
    "password": "<DB_USER_PASSWORD>",
    "name": "<DB_NAME>",
    "port": 5432,
    "sslmode": false,
    "timezone": "Europe/London",
    "default_phase_rw_access": {
      "phase": 0,
      "read": true,
      "write": true
    },
    "phase_rw_access": []
  },
  "email": {
    "debug": true,
    "host": "smtp.gmail.com",
    "port": 587,
    "from": "<EMAIL_FROM_ADDRESS>",
    "from_name": "<EMAIL_FROM_NAME>",
    "password": "<EMAIL_PASSWORD>",
    "template_configs": {
      "templates/measure.html": {
        "max_image_width": 1024,
        "max_image_height": 400,
        "debug_to": [],
        "to": [],
        "subject_format": "Robo-scout's picks of the week 2/1/2006",
        "attachment_name_format": "picks_of_the_week_2006_01_02",
        "send_retries": 15,
        "send_backoff":  "30s",
        "html2text_options": {
          "TextOnly": true
        },
        "plain_only": true,
        "send_day": 1
      },
      "templates/started.txt": {
        "debug_to": [],
        "to": [],
        "subject_format": "Robo-scout has started Scout procedure for 2/1/2006",
        "send_retries": 15,
        "send_backoff": "30s",
        "plain_only": true
      },
      "templates/error.txt": {
        "debug_to": [],
        "to": [],
        "subject_format": "Robo-scout has encountered an error",
        "send_retries": 15,
        "send_backoff": "30s",
        "plain_only": true
      },
      "templates/finished.txt": {
        "debug_to": [],
        "to": [],
        "subject_format": "Robo-scout has finished the Scout procedure for 2/1/2006",
        "send_retries": 15,
        "send_backoff": "30s",
        "plain_only": true
      }
    }
  },
  "tasks": {
    "default_queue": "game-scout-tasks",
    "results_expire_in": 3600,
    "broker": "redis://localhost:6379",
    "result_backend": "redis://localhost:6379",
    "redis": {
      "max_idle": 3,
      "idle_timeout": 240,
      "read_timeout": 15,
      "write_timeout": 15,
      "connect_timeout": 15,
      "normal_tasks_poll_period":  1000,
      "delayed_tasks_poll_period": 500
    },
    "periodic_task_signatures": {
      "scout": {
        "args": [
          {"name": "batchSize", "type": "int", "value": 100},
          {"name": "discoveryTweets", "type": "int", "value": 30250}
        ],
        "cron": "0 10 * * *",
        "retry_count": 3
      }
    }
  },
  "twitter": {
    "api_key": "<TWITTER_API_KEY>",
    "api_key_secret": "<TWITTER_API_SECRET>",
    "bearer_token": "<TWITTER_API_BEARER_TOKEN>",
    "username": "lordandygello@gmail.com",
    "password": "<TWITTER_PASSWORD>",
    "hashtags": [],
    "blacklisted_hashtags": [],
    "headless": false
  },
  "scrape": {
    "debug": true,
    "storefronts": [
      {
        "storefront": "S",
        "tags": {
          "default_value": 10,
          "upvotes_threshold": 25,
          "values": {}
        }
      }
    ],
    "constants": {
      "transform_tweet_workers": 5,
      "update_developer_workers": 5,
      "max_update_tweets": 11,
      "seconds_between_discovery_batches": "3s",
      "seconds_between_update_batches": "30s",
      "max_total_discovery_tweets_daily_percent": 0.55,
      "max_enabled_developers_after_enable_phase": 2250,
      "max_enabled_developers_after_disable_phase": 2062,
      "max_developers_to_enable": 188,
      "percentage_of_disabled_developers_to_delete": 0.025,
      "stale_developer_days": 70,
      "discovery_game_scrape_workers": 10,
      "discovery_max_concurrent_game_scrape_workers": 9,
      "update_game_scrape_workers": 10,
      "update_max_concurrent_game_scrape_workers": 9,
      "min_scrape_storefronts_for_game_worker_wait_time": "100ms",
      "max_scrape_storefronts_for_game_worker_wait_time": "500ms",
      "max_games_per_tweet": 10,
      "max_trend_workers": 10,
      "max_trending_developers":10,
      "max_top_steam_apps": 10,
      "sock_steam_app_scrape_workers": 5,
      "sock_max_concurrent_steam_app_scrape_workers": 4,
      "sock_max_steam_app_scraper_jobs": 2048,
      "sock_steam_app_scraper_jobs_per_minute": 15,
      "sock_steam_app_scraper_drop_jobs_threshold": 512,
      "sock_min_steam_app_scraper_wait_time": "100ms",
      "sock_max_steam_app_scraper_wait_time": "500ms",
      "sock_steam_app_scrape_consumers": 10,
      "sock_dropper_wait_time": "1m",
      "scrape_max_tries": 3,
      "scrape_min_delay": "2s"
    }
  },
  "SteamWebPipes": {
    "BinaryLocation": "ScoutWebPipes/bin/Debug/<OS_ARCH>/publish/ScoutWebPipes", 
    "Location": "ws://0.0.0.0:8181", 
    "DatabaseConnectionString": "", 
    "X509Certificate": "cert.pfx"
  }
}
```

Remember to update all the values containing a placeholder (e.g. `<TWITTER_PASSWORD>`), to their actual values, and set the `debug` and `headless` flags accordingly.

### DB

This contains the DB settings. Game-scout uses [GORM](https://gorm.io/) as an ORM for a [PostgreSQL](https://www.postgresql.org/) DB to store its findings. Please note that (at the moment) there is **no option to use another DB server**.

| Key                       | Type     | Description                                                                             |
|---------------------------|----------|-----------------------------------------------------------------------------------------|
| `host`                    | `string` | The hostname/address of the PostgreSQL DB that [GORM](https://gorm.io/) will connect to |
| `user`                    | `string` | The username of the user that will be used to connect to the PostgreSQL DB              |
| `password`                | `string` | The password of the user that will be used to connect to the DB                         |
| `name`                    | `string` | The name of the DB to connect to                                                        |
| `port`                    | `int`    | The port on which the PostgreSQL server is running                                      |
| `sslmode`                 | `bool`   | Whether the PostgreSQL server is running in SSL mode                                    |
| `timezone`                | `string` | The timezone which the PostgreSQL server uses                                           |
| `default_phase_rw_access` | `object` | This isn't used just yet                                                                |
| `phase_rw_access`         | `array`  | This isn't used just yet                                                                |

### Email

This contains the settings for sending emails and email templates.

| Key                | Type                                                 | Description                                                                                  |
|--------------------|------------------------------------------------------|----------------------------------------------------------------------------------------------|
| `debug`            | `bool`                                               | Whether to send the templates to the `debug_to` addresses rather than the `to` addresses     |
| `host`             | `string`                                             | The hostname/address of the SMTP server to send emails to                                    |
| `from`             | `string`                                             | The email address that we will be sending emails from                                        |
| `from_name`        | `string`                                             | The name of the sender                                                                       |
| `password`         | `string`                                             | The password of the sender                                                                   |
| `template_configs` | `map[email.TemplatePath]*TemplateConfig` as `object` | All the [configurations for the templates](#template-config) that will be sent by game-scout |

#### Template config

| Key                      | Type                            | Description                                                                                                                                                  |
|--------------------------|---------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `max_image_width`        | `int`                           | Maximum width of any converted base64 images that are going to be displayed in the template                                                                  |
| `max_image_height`       | `int`                           | Maximum height of any converted base64 images that are going to be displayed in the template                                                                 |
| `debug_to`               | `[]string`                      | Recipients that this template will be sent to when the email `debug` flag is set                                                                             |
| `to`                     | `[]string`                      | Recipients that this template will be sent to when the email `debug` flag is **not** set                                                                     |
| `subject_format`         | `string`                        | Time format of the subject of the emails sent for this template                                                                                              |
| `attachment_name_format` | `string`                        | Time format of the filename of the file that will be attached to emails sent for this template. **File extensions should not be included**                   |
| `send_retries`           | `int`                           | Number of times the SMTP send method should be retried. If it is negative then it will be retried forever                                                    |
| `send_backoff`           | `time.Duration` as `string`     | Duration that will be multiplied by the current retry number to produce a wait time that will be slept for when an error occurs whilst sending this template |
| `html2text_options`      | `html2text.Options` as `object` | `html2text.Options` used when sending instances of this template in an email                                                                                 |
| `plain_only`             | `bool`                          | Whether to only send the plain-text when sending instances of this template in an email. If it is not set then HTML content will also be added               |
| `send_day`               | `time.Weekday` as `int`         | The `time.Weekday` that instances of this template are sent. This is ignored if the email `debug` flag is set                                                |

### Tasks

This contains the settings for the [machinery](https://github.com/RichardKnop/machinery) task manager and scheduled tasks. Game-scout is designed to run with [Redis](https://redis.io/) as a broker. However, because [machinery supports a wider range of brokers](https://github.com/RichardKnop/machinery#broker) it might be possible to extend this functionality in the future.

| Key                        | Type                                           | Description                                                                                                                                                     |
|----------------------------|------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `default_queue`            | `string`                                       | The name of the default queue in redis to push tasks to                                                                                                         |
| `results_expire_in`        | `int`                                          | How long to store results for in seconds                                                                                                                        |
| `broker`                   | `string`                                       | The address to the broker                                                                                                                                       |
| `result_backend`           | `string`                                       | The address to the result backend                                                                                                                               |
| `redis`                    | `config.RedisConfig` as `object`               | [See this file](https://github.com/RichardKnop/machinery/blob/master/v1/config/config.go) for more details                                                      |
| `periodic_task_signatures` | `map[string]PeriodicTaskSignature` as `object` | All the [configurations for the periodic tasks](#periodic-task-signature) that will be run. At the moment the only task that is registered is the `scout` task. |

#### Periodic task signature

| Key           | Type                      | Description                                        |
|---------------|---------------------------|----------------------------------------------------|
| `args`        | `[]tasks.Arg` as `object` | The arguments for this periodic task               |
| `cron`        | `string`                  | The cron string                                    |
| `retry_count` | `int`                     | The number of times to retry this task if it fails |

### Twitter

This contains the settings for the Twitter API and the Discovery phase of the Scout procedure. Game-scout requires the details of your developer Twitter account (the one to which your API keys are bound to). Game-scout uses these to determine your Tweet cap/how many Tweets you can fetch for the current month.

| Key                    | Type       | Description                                                                                                                                                                                                                                 |
|------------------------|------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `api_key`              | `string`   | Your Twitter API key                                                                                                                                                                                                                        |
| `api_key_secret`       | `string`   | Your Twitter API secret                                                                                                                                                                                                                     |
| `bearer_token`         | `string`   | Your Twitter API bearer token                                                                                                                                                                                                               |
| `username`             | `string`   | Your Twitter login username/email. This is used for determining your current Tweet cap                                                                                                                                                      |
| `password`             | `string`   | Your Twitter login password. This is used for determining your current Tweet cap                                                                                                                                                            |
| `hashtags`             | `[]string` | The hashtags to search for in the Discovery phase. Note: that you need to take into account your Twitter dev account's [query limitations](https://developer.twitter.com/en/docs/twitter-api/tweets/search/integrate/build-a-query#limits). |
| `blacklisted_hashtags` | `[]string` | The hashtags to blacklist in the Discovery phase. Note: that you need to take into account your Twitter dev account's [query limitations](https://developer.twitter.com/en/docs/twitter-api/tweets/search/integrate/build-a-query#limits).  |
| `headless`             | `bool`     | Whether to run the playwright browser that is used to fetch your Tweet cap in headless mode                                                                                                                                                 |

### Scrape

This contains the settings for the scrape procedures that are run for the different supported Storefronts, as well as general configuration settings/constants for game-scout. At the moment, the only supported Storefront is [Steam](https://store.steampowered.com/).

| Key           | Type                             | Description                                                                                                                |
|---------------|----------------------------------|----------------------------------------------------------------------------------------------------------------------------|
| `debug`       | `bool`                           | Used as a sort of global debug flag throughout the game-scout system                                                       |
| `storefronts` | `[]*StorefrontConfig` as `array` | The configurations for each supported storefront. See the [Storefront config](#storefront-config) section for more details |
| `constants`   | `ScrapeConstants` as `object`    | The constants used throughout the Scout procedure as well as the ScoutWebPipes co-process                                  |

#### Storefront config

| Key          | Type                            | Description                                       |
|--------------|---------------------------------|---------------------------------------------------|
| `storefront` | `models.Storefront` as `string` | The models.Storefront that this config applies to |
| `tags`       | `*TagConfig` as `object`        | The [tag config](#tag-config) for this Storefront |

##### Tag config

| Key                 | Type                 | Description                                                                                                                                                                                                                                                                                               |
|---------------------|----------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `default_value`     | `float64`            | Default value for tags that are not included in the `values` map. The value of each tag for a models.Game (on the models.SteamStorefront) will be multiplied by the number of upvotes it has then accumulated. The average of this accumulated value will be used to calculate models.Game.WeightedScore. |
| `upvotes_threshold` | `float64`            | The threshold for the number of upvotes a tag should have (i.e. >=) to be included in the accumulated value of all the tags for a models.Game. If this is not set (== 0), then all the tags will be added.                                                                                                |
| `values`            | `map[string]float64` | Map of tag names to values. It is useful for soft "banning" tags that we don't want to see in a game.                                                                                                                                                                                                     |

#### Scrape constants

The defaults for these values (shown [here](#configuration)), are based off of a Twitter developer account with [Elevated access](https://developer.twitter.com/en/docs/twitter-api/getting-started/about-twitter-api#v2-access-level).

| Key                                                | Type                        | Description                                                                                                                                                                                                                                                                                                             | You should use these as a default (when using a Twitter API account with Elevated access)  |
|----------------------------------------------------|-----------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------|
| `transform_tweet_workers`                          | `int`                       | Number of `transformTweetWorker` that will be spun up in the DiscoveryBatch.                                                                                                                                                                                                                                            | `5`                                                                                        |
| `update_developer_workers`                         | `int`                       | Number of `updateDeveloperWorker` that will be spun up in the Update phase.                                                                                                                                                                                                                                             | `5`                                                                                        |
| `max_update_tweets`                                | `int`                       | Maximum number of tweets fetched for each `models.Developer` in the Update phase.                                                                                                                                                                                                                                       | `11`                                                                                       |
| `seconds_between_discovery_batches`                | `time.Duration` as `string` | Number of seconds to sleep between `DiscoveryBatch` batches.                                                                                                                                                                                                                                                            | `3s`                                                                                       |
| `seconds_between_update_batches`                   | `time.Duration` as `string` | Number of seconds to sleep between queue batches of `updateDeveloperJob`.                                                                                                                                                                                                                                               | `30s`                                                                                      |
| `max_total_discovery_tweets_daily_percent`         | `float64`                   | Maximum percentage that the `discoveryTweets` (parameter to the `Scout` procedure) number can be out of `myTwitter.TweetsPerDay`.                                                                                                                                                                                       | `0.55`                                                                                     |
| `max_enabled_developers_after_enable_phase`        | `float64`                   | Number of enabled `models.Developer`s that should exist after the Enable phase.                                                                                                                                                                                                                                         | `maxTotalUpdateTweets / maxUpdateTweets = 2250`                                            |
| `max_enabled_developers_after_disable_phase`       | `float64`                   | Number of `models.Developer`s to keep in the Disable phase.                                                                                                                                                                                                                                                             | `floor(maxEnabledDevelopersAfterEnablePhase * 0.9165) = 2062`                              |
| `max_developers_to_enable`                         | `float64`                   | Maximum number of `models.Developer`s that can be re-enabled in the Enable phase.                                                                                                                                                                                                                                       | `ciel(maxEnabledDevelopersAfterEnablePhase - maxEnabledDevelopersAfterDisablePhase) = 188` |
| `percentage_of_disabled_developers_to_delete`      | `float64`                   | Percentage of all disabled `models.Developer`s to delete in the Delete phase.                                                                                                                                                                                                                                           | `0.025`                                                                                    |
| `stale_developer_days`                             | `int`                       | Number of days after which a developer can become stale if their latest snapshot was created `stale_developer_days` ago.                                                                                                                                                                                                | `70`                                                                                       |
| `discovery_game_scrape_workers`                    | `int`                       | Number of `models.StorefrontScrapers` to start in the Discovery phase.                                                                                                                                                                                                                                                  | `10`                                                                                       |
| `discovery_max_concurrent_game_scrape_workers`     | `int`                       | Number of `models.StorefrontScrapers` that can be processing a job at the same time in the Discovery Phase.                                                                                                                                                                                                             | `9`                                                                                        |
| `update_game_scrape_workers`                       | `int`                       | Number of `models.StorefrontScrapers` to start in the Update phase.                                                                                                                                                                                                                                                     | `10`                                                                                       |
| `update_max_concurrent_game_scrape_workers`        | `int`                       | Number of `models.StorefrontScrapers` that can be processing a job at the same time in the Update Phase.                                                                                                                                                                                                                | `9`                                                                                        |
| `min_scrape_storefronts_for_game_worker_wait_time` | `time.Duration` as `string` | Minimum amount of time for a `models.StorefrontScrapers` to wait after completing a job in the Discovery and Update Phase.                                                                                                                                                                                              | `100ms`                                                                                    |
| `max_scrape_storefronts_for_game_worker_wait_time` | `time.Duration` as `string` | Maximum amount of time for a `models.StorefrontScrapers` to wait after completing a job in the Discovery and Update Phase.                                                                                                                                                                                              | `500ms`                                                                                    |
| `max_games_per_tweet`                              | `int`                       | Maximum number of games for each tweet we process in the Discovery and Update Phase. This is needed so that we don't overload the queue to the `models.StorefrontScrapers`.                                                                                                                                             | `10`                                                                                       |
| `max_trend_workers`                                | `int`                       | Maximum number of `trendFinder` workers to start to find the `models.Trend`, `models.DeveloperSnapshot`, and `models.Game` for a `models.Developer` to use in the `models.TrendingDev` field in `email.MeasureContext` for the `email.Measure` email `email.Template`.                                                  | `10`                                                                                       |
| `max_trending_developers`                          | `int`                       | Maximum number of `models.TrendingDev` to feature within the `email.Measure` email `email.Template`.                                                                                                                                                                                                                    | `10`                                                                                       |
| `max_top_steam_apps`                               | `int`                       | Maximum number of `models.SteamApp` to feature within the `email.Measure` email `email.Template`.                                                                                                                                                                                                                       | `10`                                                                                       |
| `sock_steam_app_scrape_workers`                    | `int`                       | Number of `models.StorefrontScrapers` to start in the goroutine that will watch the output of the `ScoutWebPipes` co-process.                                                                                                                                                                                           | `5`                                                                                        |
| `sock_max_concurrent_steam_app_scrape_workers`     | `int`                       | Number of `models.StorefrontScrapers` that can be processing a job at the same time in the websocket client.                                                                                                                                                                                                            | `4`                                                                                        |
| `sock_max_steam_app_scraper_jobs`                  | `int`                       | Maximum number of jobs that can be queued for the `models.StorefrontScrapers` instance in the websocket client.                                                                                                                                                                                                         | `2048`                                                                                     |
| `sock_steam_app_scraper_jobs_per_minute`           | `int`                       | Number of `models.SteamApp` the websocket client should be completing every minute. If it falls below this threshold, and the number of jobs in the `models.StorefrontScrapers` queue is above `sock_steam_app_scraper_drop_jobs_threshold` then the dropper goroutine will drop some jobs the next time it is running. | `15`                                                                                       |
| `sock_steam_app_scraper_drop_jobs_threshold`       | `int`                       | Threshold of jobs in the `models.StorefrontScrapers` at which the dropper goroutine will start to drop jobs to make room.                                                                                                                                                                                               | `sockMaxSteamAppScraperJobs / 4 = 512`                                                     |
| `sock_min_steam_app_scraper_wait_time`             | `time.Duration` as `string` | Minimum amount of time for a `models.StorefrontScrapers` to wait after completing a job in the websocket client.                                                                                                                                                                                                        | `100ms`                                                                                    |
| `sock_max_steam_app_scraper_wait_time`             | `time.Duration` as `string` | Maximum amount of time for a `models.StorefrontScrapers` to wait after completing a job in the websocket client.                                                                                                                                                                                                        | `500ms`                                                                                    |
| `sock_steam_app_scrape_consumers`                  | `int`                       | Number of consumers to start in the websocket client to consume the scraped `models.SteamApp` from the `models.StorefrontScrapers` instance.                                                                                                                                                                            | `10`                                                                                       |
| `sock_dropper_wait_time`                           | `time.Duration` as `string` | Amount time for the dropper goroutine to wait each time it has been run.                                                                                                                                                                                                                                                | `1m`                                                                                       |
| `scrape_max_tries`                                 | `int`                       | Maximum number of tries that is passed to a `models.GameModelStorefrontScraper` from `models.ScrapeStorefrontForGameModel` in a `models.StorefrontScrapers` instance. This affects the number of times a certain scrape procedure will be retried for a certain `models.Storefront`.                                    | `3`                                                                                        |
| `scrape_min_delay`                                 | `time.Duration` as `string` |                                                                                                                                                                                                                                                                                                                         | `2s`                                                                                       |

### Steam web pipes

This contains the settings for the Steam web pipes co-process. This is an additional process spawned off when running `go run . worker`/`game-scout worker` that will connect to the Steam PICS changelist network and publish any changelists that it captures to a websocket that will be scraped by a goroutine listening as a client on this websocket. It is used to determine newly updated Steam store pages by favouring apps with a lower number of total updates (see `models.SteamApp`). The code for this co-process is taken from the [SteamWebPipes repository](https://github.com/xPaw/SteamWebPipes) by xPaw.

| Key                        | Type     | Description                                                                                                                                                                     |
|----------------------------|----------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `BinaryLocation`           | `string` | The relative location to the built `ScoutWebPipes`/`SteamWebPipes` binary                                                                                                       |
| `Location`                 | `string` | The address on which to start the websocket                                                                                                                                     |
| `DatabaseConnectionString` | `string` | This is only provided for completeness for the `SteamWebPipes` process and is not used as we keep track of any `models.SteamApp`s that we scrape in game-scout's PostgreSQL DB. |
| `X509Certificate`          | `string` | The location to the X509 certification                                                                                                                                          |
