# game-scout

A tool that tracks indie developers on Twitter and highlights up and coming developers

<!-- TOC -->
* [Configuration](#configuration)
  * [DB](#db)
  * [Email](#email)
    * [Template config](#template-config)
  * [Tasks](#tasks)
    * [Periodic task signature](#periodic-task-signature)
  * [Twitter](#twitter)
    * [Twitter rate limits](#twitter-rate-limits)
  * [Reddit](#reddit)
    * [Reddit rate limits](#reddit-rate-limits)
  * [Monday](#monday)
    * [MondayConfig](#mondayconfig)
    * [MondayMappingConfig](#mondaymappingconfig)
  * [Scrape](#scrape)
    * [Weighted model field evaluators](#weighted-model-field-evaluators)
    * [Storefront config](#storefront-config)
      * [Tag config](#tag-config)
    * [Scrape constants](#scrape-constants)
  * [Steam web pipes](#steam-web-pipes)
<!-- TOC -->

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
    "postgres_db_name": "postgres",
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
    "username": "<TWITTER_USERNAME/EMAIL>",
    "password": "<TWITTER_PASSWORD>",
    "hashtags": [],
    "blacklisted_hashtags": [],
    "headless": false,
    "rate_limit": {
      "tweets_per_month": 1650000,
      "tweets_per_week": 385000,
      "tweets_per_day": 55000,
      "tweets_per_hour": 2200,
      "tweets_per_minute": 1000,
      "tweets_per_second": 200,
      "time_per_request": "500ms"
    },
    "tweet_cap_location":"tweetCap.json",
    "created_at_format": "2006-01-02T15:04:05.000Z",
    "ignored_error_types": [
      "https://api.twitter.com/2/problems/resource-not-found",
      "https://api.twitter.com/2/problems/not-authorized-for-resource"
    ]
  },
  "reddit": {
    "personal_use_script": "<PERSONAL_USE_SCRIPT_ID>",
    "secret": "<PERSONAL_USE_SCRIPT_SECRET>",
    "user_agent": "<PERSONAL_USE_SCRIPT_USER_AGENT>",
    "username": "<REDDIT_USERNAME>",
    "password": "<REDDIT_PASSWORD>",
    "subreddits": [],
    "rate_limits": {
      "requests_per_month": 2592000,
      "requests_per_week": 604800,
      "requests_per_day": 86400,
      "requests_per_hour": 3600,
      "requests_per_minute": 60,
      "requests_per_second": 1,
      "time_per_request": "1s"
    }
  },
  "monday": {
    "token": "<MONDAY_API_KEY>",
    "mapping": {
      "models.Game": {
        "model_name": "models.Game",
        "board_ids": [0],
        "group_ids": ["games_from_twitter"],
        "model_instance_id_column_id": "id",
        "model_instance_upvotes_column_id": "upvotes",
        "model_instance_downvotes_column_id": "downvotes",
        "model_instance_watched_column_id": "watched",
        "model_field_to_column_value_expr": {
          "storefront": "build_status_label(game.Storefront.String())",
          "developer_website": "build_link(len(game.VerifiedDeveloperUsernames) > 0 ? sprintf(\"https://twitter.com/%s\", game.GetVerifiedDeveloperUsernames()[0]) : \"\")",
          "created_at": "build_date(now())",
          "website": "build_link(game.Website.IsValid() ? game.Website.String : \"\")",
          "id": "game.ID.String()",
          "last_fetched": "build_date(now())"
        },
        "columns_to_update": ["last_fetched"]
      },
      "models.SteamApp": {
        "model_name": "models.SteamApp",
        "board_ids": [0],
        "group_ids": ["steam_apps"],
        "model_instance_id_column_id": "id",
        "model_instance_upvotes_column_id": "upvotes",
        "model_instance_downvotes_column_id": "downvotes",
        "model_instance_watched_column_id": "watched",
        "model_field_to_column_value_expr": {
          "storefront": "build_status_label(\"Steam\")",
          "developer_website": "build_link(game.VerifiedDeveloper(db()) != nil ? game.VerifiedDeveloper(db()).Link() : \"\")",
          "created_at": "build_date(now())",
          "website": "build_link(game.Website())",
          "id": "str(game.ID)",
          "last_fetched": "build_date(now())"
        },
        "columns_to_update": ["last_fetched"]
      }
    }
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
      },
      {
        "storefront": "I",
        "tags": {
          "default_value": 500,
          "upvotes_threshold": 0,
          "values": {}
        }
      }
    ],
    "weighted_model_expressions": {
      "DeveloperSnapshot": [
        {
          "model_name":"DeveloperSnapshot",
          "field": "Tweets",
          "weight": 0.65,
          "expression": "scale_range(clamp(float(field.Val), 100.0), 0.0, 100.0, 100_000.0, -500_000.0)"
        },
        {
          "model_name": "DeveloperSnapshot",
          "field": "TweetTimeRange",
          "weight": 0.35,
          "expression": "field.Ptr.IsValid() ? scale_range(clamp_min_max(field.Ptr.Ptr().Minutes(), 1.0, 10_000.0), 1.0, 10_000.0, -10_000.0, 10_000.0) : -10_000.0"
        },
        {
          "model_name": "DeveloperSnapshot",
          "field": "AverageDurationBetweenTweets",
          "weight": 0.45,
          "expression": "field.Ptr.IsValid() ? scale_range(clamp_min_max(field.Ptr.Ptr().Minutes(), 1.0, 10_000.0), 1.0, 10_000.0, -10_000.0, 10_000.0) : -10_000.0"
        },
        {
          "model_name": "DeveloperSnapshot",
          "field": "TweetsPublicMetrics",
          "weight": 0.75,
          "expression": "field.Val != nil ? [clamp(float(field.Val.Impressions), 1000.0), clamp(float(field.Val.URLLinkClicks), 1000.0), clamp(float(field.Val.UserProfileClicks), 1000.0), clamp(float(field.Val.Likes), 1000.0), clamp(float(field.Val.Replies), 1000.0), clamp(float(field.Val.Retweets), 1000.0), clamp(float(field.Val.Quotes), 1000.0)] : 0.0"
        },
        {
          "model_name": "DeveloperSnapshot",
          "field": "PostPublicMetrics",
          "weight": 0.8,
          "expression": "field.Val != nil ? [clamp(float(field.Val.Ups), 1000.0), clamp(float(field.Val.Downs), 1000.0), clamp(float(field.Val.Score), 1000.0), clamp(float(field.Val.UpvoteRatio), 1000.0), clamp(float(field.Val.NumberOfComments), 1000.0), clamp(float(field.Val.SubredditSubscribers), 1000.0)] : 0.0"
        },
        {
          "model_name": "DeveloperSnapshot",
          "field": "UserPublicMetrics",
          "weight": 0.45,
          "expression": "field.Val != nil ? [clamp(float(field.Val.Followers), 1000.0), clamp(float(field.Val.Following), 1000.0), scale_range(clamp(float(field.Val.Tweets), 10_000.0), 0.0, 10_000.0, 100_000.0, -1_000_000.0), clamp(float(field.Val.Listed), 1000.0)] : 0.0"
        },
        {
          "model_name": "DeveloperSnapshot",
          "field": "RedditPublicMetrics",
          "weight": 0.55,
          "expression": "field.Val != nil ? [clamp(float(field.Val.PostKarma), 5000.0), clamp(float(field.Val.CommentKarma), 5000.0)] : 0.0"
        },
        {
          "model_name": "DeveloperSnapshot",
          "field": "ContextAnnotationSet",
          "weight": 0.55,
          "expression": "field.Val != nil ? map(field.Val.ToSlice(), {#.Domain.Value()}) : 0.0"
        },
        {
          "model_name": "DeveloperSnapshot",
          "field": "Games",
          "weight": 0.7,
          "expression": "field.Val == 0.0 ? -15_000_000.0 : scale_range(clamp(float(field.Val), 100.0), 1.0, 100.0, 100_000.0, -5_000_000.0)"
        },
        {
          "model_name": "DeveloperSnapshot",
          "field": "GameWeightedScoresSum",
          "weight": 0.8,
          "expression": "model.Games > 0 ? field.Val / float(model.Games) : 0.0"
        },
        {
          "model_name": "DeveloperSnapshot",
          "field": "TimesHighlighted",
          "weight": 0.9,
          "expression": "float(field.Val) * -10000.0"
        }
      ],
      "Game": [
        {
          "model_name": "Game",
          "field": "Publisher",
          "weight": 0.55,
          "expression": "field.Val.IsValid() ? -100000.0 : 4000.0"
        },
        {
          "model_name": "Game",
          "field": "TotalReviews",
          "weight": 0.75,
          "expression": "field.Val.IsValid() ? scale_range(clamp(float(field.Val.Int32 + 1), 5000.0), 1.0, 5000.0, 1000000.0, -1000000.0) : 0.0"
        },
        {
          "model_name": "Game",
          "field": "ReviewScore",
          "weight": 0.65,
          "expression": "field.Val.IsValid() ? field.Val.Float64 * 1500.0 : 0.0"
        },
        {
          "model_name": "Game",
          "field": "TotalUpvotes",
          "weight": 0.45,
          "expression": "field.Val.IsValid() ? scale_range(clamp(float(field.Val.Int32 * 2), 5000.0) * 2.0, 0.0, 10000.0, -1000.0, 10000.0) : 0.0"
        },
        {
          "model_name": "Game",
          "field": "TotalDownvotes",
          "weight": 0.25,
          "expression": "field.Val.IsValid() ? scale_range(clamp(float(field.Val.Int32 * 2), 5000.0) * 2.0, 0.0, 10000.0, -1000.0, 10000.0) : 0.0"
        },
        {
          "model_name": "Game",
          "field": "TotalComments",
          "weight": 0.35,
          "expression": "field.Val.IsValid() ? scale_range(clamp(float(field.Val.Int32 * 2), 5000.0) * 2.0, 0.0, 10000.0, -1000.0, 10000.0) : 0.0"
        },
        {
          "model_name": "Game",
          "field": "TagScore",
          "weight": 0.25,
          "expression": "field.Val.IsValid() ? field.Val.Float64 : 0.0"
        },
        {
          "model_name": "Game",
          "field": "Updates",
          "weight": -0.15,
          "expression": "float(field.Val + 1) / 1500.0"
        },
        {
          "model_name": "Game",
          "field": "ReleaseDate",
          "weight": 0.7,
          "expression": "field.Val.IsValid() && !field.Val.Time.IsZero() ? (abs(int(field.Val.Time.Sub(now())) - int(duration(\"month\"))) > int(duration(\"month\")) * 5 ? duration(boolMap(-1, 1)[int(field.Val.Time.Sub(now())) - int(duration(\"month\")) < 0] * int(duration(\"month\")) * 5).Hours() : duration(int(field.Val.Time.Sub(now())) - int(duration(\"month\"))).Hours()) : 0.0"
        },
        {
          "model_name": "Game",
          "field": "Votes",
          "weight": 0.8,
          "expression": "float(field.Val * 100000)"
        }
      ],
      "SteamApp": [
        {
          "model_name": "SteamApp",
          "field": "Publisher",
          "weight": 0.55,
          "expression": "field.Val.IsValid() ? -120000.0 : 4000.0"
        },
        {
          "model_name": "SteamApp",
          "field": "TotalReviews",
          "weight": 0.75,
          "expression": "scale_range(clamp(float(field.Val + 1), 5000.0), 1.0, 5000.0, 1000000.0, -1000000.0)"
        },
        {
          "model_name": "SteamApp",
          "field": "ReviewScore",
          "weight": 0.65,
          "expression": "field.Val * 1500.0"
        },
        {
          "model_name": "SteamApp",
          "field": "TotalUpvotes",
          "weight": 0.45,
          "expression": "scale_range(clamp(float(field.Val), 5000.0) * 2.0, 0.0, 10000.0, -1000.0, 10000.0)"
        },
        {
          "model_name": "SteamApp",
          "field": "TotalDownvotes",
          "weight": 0.25,
          "expression": "scale_range(clamp(float(field.Val), 5000.0) * 2.0, 0.0, 10000.0, -1000.0, 10000.0)"
        },
        {
          "model_name": "SteamApp",
          "field": "TotalComments",
          "weight": 0.35,
          "expression": "scale_range(clamp(float(field.Val), 5000.0) * 2.0, 0.0, 10000.0, -1000.0, 10000.0)"
        },
        {
          "model_name": "SteamApp",
          "field": "TagScore",
          "weight": 0.25,
          "expression": "field.Val == 0 && model.ReleaseDate.After(now()) ? 5000.0 : field.Val"
        },
        {
          "model_name": "SteamApp",
          "field": "Updates",
          "weight": -0.55,
          "expression": "field.Val == 0 ? float(field.Val + 1) / 150000.0 : float(field.Val + 1) / 1500.0"
        },
        {
          "model_name": "SteamApp",
          "field": "AssetModifiedTime",
          "weight": 0.2,
          "expression": "scale_range(duration(clamp(float(now().Sub(field.Val)), float(duration(\"month\")) * 5.0)).Hours(), 0.0, 24.0 * 30.0 * 5.0, 2000.0, 0.0)"
        },
        {
          "model_name": "SteamApp",
          "field": "CreatedAt",
          "weight": 0.8,
          "expression": "scale_range(duration(clamp(float(now().Sub(field.Val)), float(duration(\"month\")) * 5.0)).Hours(), 0.0, 24.0 * 30.0 * 5.0, 2000.0, 0.0)"
        },
        {
          "model_name": "SteamApp",
          "field": "ReleaseDate",
          "weight": 0.6,
          "expression": "duration(abs(int(field.Val.Sub(now())) - int(duration(\"month\"))) > int(duration(\"month\")) * 5 ? boolMap(-1, 1)[int(field.Val.Sub(now())) - int(duration(\"month\")) < 0] * int(duration(\"month\")) * 5 : int(field.Val.Sub(now())) - int(duration(\"month\"))).Hours()"
        },
        {
          "model_name": "SteamApp",
          "field": "TimesHighlighted",
          "weight": 0.9,
          "expression": "float(field.Val) * -10000.0"
        },
        {
          "model_name": "SteamApp",
          "field": "OwnersOnly",
          "weight": 0.7,
          "expression": "boolMap(-100000.0, 100.0)[field.Val]"
        },
        {
          "model_name": "SteamApp",
          "field": "HasStorepage",
          "weight": 0.7,
          "expression": "boolMap(100.0, -50000.0)[field.Val]"
        },
        {
          "model_name": "SteamApp",
          "field": "Votes",
          "weight": 0.8,
          "expression": "float(field.Val * 100000)"
        }
      ]
    },
    "constants": {
      "scout_timeout": "5h",
      "transform_tweet_workers": 5,
      "reddit_subreddit_scrape_workers": 5,
      "reddit_discovery_batch_workers": 5,
      "reddit_binding_paginator_wait_time": "500ms",
      "reddit_comments_per_post": 100,
      "reddit_posts_per_subreddit": 50,
      "update_developer_workers": 7,
      "update_producer_finished_job_timeout": "2s",
      "update_producer_finished_job_max_timeouts": 2700,
      "max_update_tweets": 11,
      "max_update_posts": 11,
      "reddit_producer_batch_multiplier": 1.3,
      "reddit_producer_batch_sleep_time": "5m",
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
      "max_games_per_post": 10,
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
    "Disable": false,
    "BinaryLocation": "ScoutWebPipes/bin/Debug/<OS_ARCH>/publish/ScoutWebPipes", 
    "Location": "ws://0.0.0.0:8181", 
    "DatabaseConnectionString": "", 
    "X509Certificate": "cert.pfx"
  }
}
```

Remember to update all the values containing a placeholder (e.g. `<TWITTER_PASSWORD>`), to their actual values, and set the `debug` and `headless` flags accordingly.

### DB

This contains the [DB](db) settings. Game-scout uses [GORM](https://gorm.io/) as an ORM for a [PostgreSQL](https://www.postgresql.org/) DB to store its findings. Please note that (at the moment) there is **no option to use another DB server**.

| Key                       | Type     | Description                                                                                                                 |
|---------------------------|----------|-----------------------------------------------------------------------------------------------------------------------------|
| `host`                    | `string` | The hostname/address of the PostgreSQL DB that [GORM](https://gorm.io/) will connect to                                     |
| `user`                    | `string` | The username of the user that will be used to connect to the PostgreSQL DB                                                  |
| `password`                | `string` | The password of the user that will be used to connect to the DB                                                             |
| `name`                    | `string` | The name of the DB to connect to                                                                                            |
| `port`                    | `int`    | The port on which the PostgreSQL server is running                                                                          |
| `sslmode`                 | `bool`   | Whether the PostgreSQL server is running in SSL mode                                                                        |
| `timezone`                | `string` | The timezone which the PostgreSQL server uses                                                                               |
| `postgres_db_name`        | `string` | The name of the main PostgreSQL database. This is used so that we can create and drop the test database when running tests. |
| `default_phase_rw_access` | `object` | **This isn't used just yet**                                                                                                |
| `phase_rw_access`         | `array`  | **This isn't used just yet**                                                                                                |

### Email

This contains the settings for sending [emails and email templates](email).

| Key                | Type                                                 | Description                                                                                  |
|--------------------|------------------------------------------------------|----------------------------------------------------------------------------------------------|
| `debug`            | `bool`                                               | Whether to send the templates to the `debug_to` addresses rather than the `to` addresses     |
| `host`             | `string`                                             | The hostname/address of the SMTP server to send emails to                                    |
| `from`             | `string`                                             | The email address that we will be sending emails from                                        |
| `from_name`        | `string`                                             | The name of the sender                                                                       |
| `password`         | `string`                                             | The password of the sender                                                                   |
| `template_configs` | `map[email.TemplatePath]*TemplateConfig` as `object` | All the [configurations for the templates](#template-config) that will be sent by game-scout |

#### Template config

Contains the configuration for a single email template (see the [email package](email), [`template.go`](email/template.go) and [`email/templates`](email/templates)). Game-scout uses the [html2text library](https://github.com/jaytaylor/html2text) to convert HTML content into plain-text to produce a plain-text version of the body for a template.

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

This contains the settings for the [machinery](https://github.com/RichardKnop/machinery) task manager and scheduled tasks. Game-scout is designed to run with [Redis](https://redis.io/) as a broker. However, because [machinery supports a wider range of brokers](https://github.com/RichardKnop/machinery#broker) it might be possible to extend this functionality in the future. The logic for most of the task scheduling in game-scout can be found in the [`tasks` package](tasks), and [`workers.go`](workers.go).

| Key                        | Type                                           | Description                                                                                                                                                     |
|----------------------------|------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `default_queue`            | `string`                                       | The name of the default queue in redis to push tasks to                                                                                                         |
| `results_expire_in`        | `int`                                          | How long to store results for in seconds                                                                                                                        |
| `broker`                   | `string`                                       | The address to the broker                                                                                                                                       |
| `result_backend`           | `string`                                       | The address to the result backend                                                                                                                               |
| `redis`                    | `config.RedisConfig` as `object`               | [See this file](https://github.com/RichardKnop/machinery/blob/master/v1/config/config.go) for more details                                                      |
| `periodic_task_signatures` | `map[string]PeriodicTaskSignature` as `object` | All the [configurations for the periodic tasks](#periodic-task-signature) that will be run. At the moment the only task that is registered is the `scout` task. |

#### Periodic task signature

Represents the task signature for a periodic task in game-scout. The only periodic task you should worry about registering is the main [`Scout`](scout.go) procedure.

| Key           | Type                      | Description                                        |
|---------------|---------------------------|----------------------------------------------------|
| `args`        | `[]tasks.Arg` as `object` | The arguments for this periodic task               |
| `cron`        | `string`                  | The cron string                                    |
| `retry_count` | `int`                     | The number of times to retry this task if it fails |

### Twitter

This contains the settings for the Twitter API and the Discovery phase of the Scout procedure. Game-scout requires the details of your developer Twitter account (the one to which your API keys are bound to). Game-scout uses these to determine your Tweet cap/how many Tweets you can fetch for the current month.

| Key                    | Type                             | Description                                                                                                                                                                                                                                                                                                               |
|------------------------|----------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `api_key`              | `string`                         | Your Twitter API key                                                                                                                                                                                                                                                                                                      |
| `api_key_secret`       | `string`                         | Your Twitter API secret                                                                                                                                                                                                                                                                                                   |
| `bearer_token`         | `string`                         | Your Twitter API bearer token                                                                                                                                                                                                                                                                                             |
| `username`             | `string`                         | Your Twitter login username/email. This is used for determining your current Tweet cap                                                                                                                                                                                                                                    |
| `password`             | `string`                         | Your Twitter login password. This is used for determining your current Tweet cap                                                                                                                                                                                                                                          |
| `hashtags`             | `[]string`                       | The hashtags to search for in the Discovery phase. Note: that you need to take into account your Twitter dev account's [query limitations](https://developer.twitter.com/en/docs/twitter-api/tweets/search/integrate/build-a-query#limits).                                                                               |
| `blacklisted_hashtags` | `[]string`                       | The hashtags to blacklist in the Discovery phase. Note: that you need to take into account your Twitter dev account's [query limitations](https://developer.twitter.com/en/docs/twitter-api/tweets/search/integrate/build-a-query#limits).                                                                                |
| `headless`             | `bool`                           | Whether to run the playwright browser that is used to fetch your Tweet cap in headless mode                                                                                                                                                                                                                               |
| `rate_limits`          | `*TwitterRateLimits` as `object` | The rate limits for each timeframe to abide by when requesting resources from the Twitter API. See the [Rate limits](#rate-limits) section for more details                                                                                                                                                               |
| `tweet_cap_location`   | `string`                         | The path (relative to the root of the repository) to save/load the Tweet cap cache to/from. This is a JSON file that contains the remaining number of Tweets for your account for the month. This is used by game-scout to know how many Tweets it can request per-day and per-month.                                     |
| `created_at_format`    | `string`                         | The time format that the Twitter API uses for the `created_at` fields of various resources. This should be set to the ISO 8601 standard i.e. `"2006-01-02T15:04:05.000Z"`.                                                                                                                                                |
| `ignored_error_types`  | `[]string`                       | The errors from the Twitter API that can be safely ignored. This is a list of URLs to the Twitter API documentation's explanations of the error to be ignored. I recommend setting this to `["https://api.twitter.com/2/problems/resource-not-found", "https://api.twitter.com/2/problems/not-authorized-for-resource"]`. |

#### Twitter rate limits

One thing to note when setting your rate limits is that it is better to be pessimistic than optimistic. To set the rate limits for my own Elevated account I used percentages, rounding down, and averages of the `tweets_per_month` and `tweets_per_second`.

| Key                 | Type                        | Description                                                                                                                                                                                                  | You should use these as a default (when using a Twitter API account with Elevated access) |
|---------------------|-----------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------|
| `tweets_per_month`  | `uint64`                    | The number of Tweets that can safely be requested per-month. I would recommend setting this to 80-85% of the Tweet cap for your account, so that you can use the rest for testing/other purposes.            | `1650000`                                                                                 |
| `tweets_per_week`   | `uint64`                    | The number of Tweets that can safely be requested per-week.                                                                                                                                                  | `tweets_per_day * 7 = 385000`                                                             |
| `tweets_per_day`    | `uint64`                    | The number of Tweets that can safely be requested per-day. I use this metric to calculate the `tweets_per_week`, `tweets_per_hour` and `tweets_per_minute`.                                                  | `tweets_per_month / 30 = 55000`                                                           |
| `tweets_per_hour`   | `uint64`                    | The number of Tweets that can safely be requested per-hour.                                                                                                                                                  | `rounded_down_to_nearest_100(tweets_per_day / 24) = 2200`                                 |
| `tweets_per_minute` | `uint64`                    | The number of Tweets that can safely be requested per-minute.                                                                                                                                                | `(tweets_per_day - tweets_per_second) / 2 = 1000`                                         |
| `tweets_per_second` | `uint64`                    | The number of Tweets that can safely be requested per-second. I set this to the maximum number of Tweets that can be requested by an endpoint that returns Tweets (100), divided by `time_per_request / 1s`. | `100 / (time_per_request / 1s) = 200`                                                     |
| `time_per_request`  | `time.Duration` as `string` | The average time that a request to the Twitter API should be completed. Set this to something that is more towards the end of "worst-case".                                                                  | `500ms`                                                                                   |

### Reddit

This contains the settings for the personal use script used to access the Reddit API in the [Discovery](discovery.go) and [Update](update.go) phases of the procedure. Game-scout requires the details of the Reddit account which provisioned the personal use script in order to authenticate on the API using OAuth.

| Key                   | Type                            | Description                                                                                                            |
|-----------------------|---------------------------------|------------------------------------------------------------------------------------------------------------------------|
| `personal_use_script` | `string`                        | The ID of the personal use script that was set up for game-scout scraping.                                             |
| `secret`              | `string`                        | The secret that must be sent to the Reddit API access-token endpoint to acquire an OAuth token.                        |
| `user_agent`          | `string`                        | Used in requests to the Reddit API to identify game-scout.                                                             |
| `username`            | `string`                        | The username for the Reddit account related to the personal use script. For use in the acquirement of the OAuth token. |
| `password`            | `string`                        | The password for the Reddit account related to the personal use script. For use in the acquirement of the OAuth token. |
| `subreddits`          | `[]string`                      | The list of subreddits to scrape. E.g. "GameDevelopment" (notice the lack of "r/" prefix).                             |
| `rate_limits`         | `*RedditRateLimits` as `object` | Contains the rate limits per-unit of time.                                                                             |

#### Reddit rate limits

Again, better to be pessimistic than optimistic here. Because the Reddit API limits requests to a 5-minute period, and there is no caps to worry about, these rate limits are solely based off of the 60 requests per-minute.

| Key                   | Type                        | Description                                                                                                                                 | You should use these as a default |
|-----------------------|-----------------------------|---------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------|
| `requests_per_month`  | `uint64`                    | The number of requests that can be made safely to the Reddit API per-month.                                                                 | `requests_per_day * 30 = 2592000` |
| `requests_per_week`   | `uint64`                    | The number of requests that can be made safely to the Reddit API per-week.                                                                  | `requests_per_day * 7 = 604800`   |
| `requests_per_day`    | `uint64`                    | The number of requests that can be made safely to the Reddit API per-day.                                                                   | `requests_per_hour * 24 = 86400`  |
| `requests_per_hour`   | `uint64`                    | The number of requests that can be made safely to the Reddit API per-hour.                                                                  | `requests_per_minute * 60 = 3600` |
| `requests_per_minute` | `uint64`                    | The number of requests that can be made safely to the Reddit API per-minute.                                                                | `60`                              |
| `requests_per_second` | `uint64`                    | The number of requests that can be made safely to the Reddit API per-second.                                                                | `requests_per_minute / 60 = 1`    |
| `time_per_request`    | `time.Duration` as `string` | The average time that a request to the Twitter API should be completed. Set this to something that is more towards the end of "worst-case". | `1s`                              |                                   |


### Monday

game-scout has a Monday integration to mirror over each [Game](db/models/game.go) and [SteamApp](db/models/steam_app.go) mentioned in the reports sent out by the Measure phase to a chosen board/group combo. This procedure uses the [expr](https://github.com/antonmedv/expr) library to map the fields in each Game/SteamApp to a column value that can be posted by the Monday API. These expressions are contained in separate mappings for each [GameModel](db/models/scrape.go) (i.e. [Game](db/models/game.go) and [SteamApp](db/models/steam_app.go)) from their Monday column ID, to the expression that will generate the column value to POST to the Monday API. These expressions are compiled on start, and are passed a `game` variable that contains either a `*models.Game` or a `*models.SteamApp`, as well as the set of following function signatures:

| Signature                                             | Description                                                                                                                                                                                                                                                               | Example                                                                                                                                                                                                                                                    |
|-------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `db() *gorm.DB`                                       | Returns the default DB connection used by game-scout to connect to the PostgreSQL ORM.                                                                                                                                                                                    | `game.VerifiedDeveloper(db())`: Calls the VerifiedDeveloper method for a Game/SteamApp which requires a `*gorm.DB` instance.                                                                                                                               |
| `str(v any) string`                                   | Returns the string representation of the given value. This is done using `fmt.Sprintf` and the `%v` verb.                                                                                                                                                                 | `str(game.ID)`: Converts the `uint64` ID of a SteamApp to a string.                                                                                                                                                                                        |
| `sprintf(format string, a ...any) string`             | `fmt.Sprintf` function.                                                                                                                                                                                                                                                   | `sprintf("https://twitter.com/%s", game.GetVerifiedDeveloperUsernames()[0])`: Constructs a URL for the Twitter handle for a Game.                                                                                                                          |
| `now() time.Time`                                     | Returns the result of `time.Now().UTC()`.                                                                                                                                                                                                                                 | `build_date(now())`: Builds a `monday.DateTime` for _**_today_**_.                                                                                                                                                                                         |
| `build_date(t time.Time) monday.DateTime`             | Returns a `monday.DateTime` that can be used when POSTing to the Monday API to set Date columns on Monday. The time segment of the given time will be zeroed so just the date, month and year is set.                                                                     | `build_date(now())`: Builds a `monday.DateTime` for _**_today_**_.                                                                                                                                                                                         |
| `build_date_time(t time.Time) monday.DateTime`        | Returns a `monday.DateTime` that can be used when POSTing to the Monday API to set DateTime columns on Monday.                                                                                                                                                            | `build_date_time(now())`: Builds a `monday.DateTime` for **_now_**.                                                                                                                                                                                        |
| `build_status_index(index int) monday.StatusIndex`    | Returns a `monday.StatusIndex` that can be used when POSTing to the Monday API to set Status columns by index on Monday. The given integer should be an index of an existing label for the mapped status column.                                                          | `build_status_index(0)`: Builds a `monday.StatusIndex` for the first label.                                                                                                                                                                                |
| `build_status_label(label string) monday.StatusLabel` | Returns a `monday.StatusLabel` that can be used when POSTing to the Monday API to set Status columns by label name on Monday. The given string should be the name of an existing label for the mapped status column.                                                      | `build_status_label(game.Storefront.String())` Builds a `monday.StatusLabel` for a Game's Storefront.                                                                                                                                                      |
| `build_link(...string) monday.Link`                   | Returns a `monday.Link` that can be used when POSTing to the Monday API to set Link columns. If one string is given then the link will not be named, if two are given then the link will have the URL of the first argument and be named whatever the second argument is. | `build_link(game.Website.IsValid() ? game.Website.String : "")`: Builds a `monday.Link` for a Game's Website, or the empty string if Website is null. Note that here the text displayed for this link will be the same as the URL to which the link links. |

The [`monday`](monday) package contains all the types and bindings that are used by game-scout to interact with the Monday API, so if you need further information on the types mentioned above as well as the specifics behind adding items to Monday boards, this is a good place to start.

#### MondayConfig

| Key            | Type                                          | Description                                                                                                                                                |
|----------------|-----------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `token`        | `string`                                      | The bearer token used to authenticate for your linked Monday organisation.                                                                                 |
| `mapping`      | `map[string]*MondayMappingConfig` as `object` | A mapping of GameModel Model names (i.e. `models.Game` or `models.SteamApp`) to [`MondayMappingConfig`s.](#mondaymappingconfig)                            |
| `test_mapping` | `map[string]*MondayMappingConfig` as `object` | A copy of `mapping` that is only used for testing. This is so you can keep the Monday boards used for testing separate from the boards used in production. |

#### MondayMappingConfig

| Key                                  | Type                            | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                        | Example                                    |
|--------------------------------------|---------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------|
| `model_name`                         | `string`                        | The name of the model that this `MondayMappingConfig` is for. This should either be "models.SteamApp" or "models.Game".                                                                                                                                                                                                                                                                                                                                            | `models.Game`/`models.SteamApp`            |
| `board_ids`                          | `[]int`                         | The Monday.com assigned IDs of the boards used to track `models.SteamApp`/`models.Game` from the Measure Phase. Newly created Monday-ified `models.Game`/`models.SteamApp` will always be added to the **first board in this list**.                                                                                                                                                                                                                               | `[1234567890]`                             |
| `group_ids`                          | `[]string`                      | The Monday.com assigned IDs of the groups within the `board_ids` used to track `models.SteamApp`/`models.Game` from the Measure Phase. _Groups that `models.Game` are in should never intersect with the boards that `models.SteamApp` are in_, **or if this is not possible**, _boards between the two `models.GameModel` should be unique_. Newly created Monday-ified `models.Game`/`models.SteamApp` will always be added to the **first group in this list**. | `["games", "downvoted_games"]`             |
| `model_instance_id_column_id`        | `string`                        | The Monday.com assigned ID of the column within the BoardID used to store the ID of the models.SteamApp/models.Game instance in game-scout.                                                                                                                                                                                                                                                                                                                        | `id`                                       |
| `model_instance_upvotes_column_id`   | `string`                        | The Monday.com assigned ID of the column within the BoardID used to store the number of upvotes of the related models.SteamApp/models.Game.                                                                                                                                                                                                                                                                                                                        | `upvotes`                                  |
| `model_instance_downvotes_column_id` | `string`                        | The Monday.com assigned ID of the column within the BoardID used to store the number of downvotes of the related models.SteamApp/models.Game.                                                                                                                                                                                                                                                                                                                      | `downvotes`                                |
| `model_instance_watched_column_id`   | `string`                        | The Monday.com assigned ID of the column within the BoardID used to store the value of the Watched field of the related models.SteamApp/models.Game. If this is set for a models.SteamApp/models.Game, then the instance will be included in a separate section of the Measure email until this flag is unset.                                                                                                                                                     | `watched`                                  |
| `model_field_to_column_value_expr`   | `map[string]string` as `object` | Represents a mapping from Monday column IDs to expressions that can be compiled using `expr.Eval` to convert a field from a given models.Game/models.SteamApp instance to a value that Monday can use. This is used when creating models.Game/models.SteamApp in their BoardID in the Measure Phase.                                                                                                                                                               | See [example config](#configuration) above |
| `columns_to_update`                  | `[]string`                      | A list of Monday column IDs to update for existing `models.SteamApp`/`models.Game` within the linked Monday `board_ids` or `group_ids`. `models.SteamApp`/`models.Game` instances that exist in the linked Monday `board_ids` or `group_ids` will be **updated at the start of the Measure Phase** using their mapped expressions in `model_field_to_column_value_expr`.                                                                                           | `["last_fetched", "link"]`                 |

### Scrape

This contains the settings for the scrape procedures that are run for the different supported Storefronts, as well as general configuration settings/constants for game-scout. At the moment, the only supported Storefronts are [Steam](https://store.steampowered.com/) and [Itch.IO](https://itch.io/).

| Key                          | Type                                                    | Description                                                                                                                                                                                                                                                                            |
|------------------------------|---------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `debug`                      | `bool`                                                  | Used as a sort of global debug flag throughout the game-scout system                                                                                                                                                                                                                   |
| `storefronts`                | `[]*StorefrontConfig` as `array`                        | The configurations for each supported storefront. See the [Storefront config](#storefront-config) section for more details                                                                                                                                                             |
| `weighted_model_expressions` | `map[string][]*WeightedModelFieldEvaluator` as `object` | Mapping of [`models.WeightedModel`](db/models/fields.go) names (with no package prefix) to an array of [`WeightedModelFieldEvaluator`s](config.go) that are used to calculate each the value of each field which contributes to the weighted score of a models.WeightedModel instance. |
| `constants`                  | `ScrapeConstants` as `object`                           | The constants used throughout the Scout procedure as well as the ScoutWebPipes co-process                                                                                                                                                                                              |

#### Weighted model field evaluators

| Key          | Type      | Description                                                                                                                                                                                                                                                                                                                                              |
|--------------|-----------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `model_name` | `string`  | The name of the [`models.WeightedModel`](db/models/fields.go) that this `WeightedModelFieldEvaluator` is for. This is not prefixed by the package name.                                                                                                                                                                                                  |
| `field`      | `string`  | The name of the `field` within the `models.WeightedModel` instance that this `WeightedModelFieldEvaluator` will evaluate.                                                                                                                                                                                                                                |
| `weight`     | `float64` | The weight of this `field`, which affects how much it influences the weighted score/weighted average for each `models.WeightedModel` instance. If this is negative, then the value produced by the evaluated `expression` will have its inverse taken before multiplying by the `weight`.                                                                |
| `expression` | `string`  | The raw source code for the expression that will be compiled by [`Compile`](config.go) to produce a `vm.Program` that is used to calculate the weighted value for this `field` for a `models.WeightedModel` instance. The `expression` should return a `float64` value, or a list of `float64` values, that have not yet had the `weight` applied to it. |


`WeightedModelFieldEvaluator`s contain expressions that calculated the value that will be used to calculate the weighted score of an instance of a [`models.WeightedModel`](db/models/fields.go). They store the `models.WeightedModel`'s name, the name of the field, the weighting of the field, and the expression that will be used to evaluate the weighted score.

The weighted score for a `models.WeightedModel` instance is a weighted average of all the values for each field with an expression. A negative weight for a field indicates that the inverse of the `models.WeightedModel` instance should be taken first before that value is multiplied by the weight.

On startup, the expressions for each field for each `models.WeightedModel` will be compiled. Each expression is passed the following variables:

- **"model"**: The `models.WeightedModel` instance.
- **"field.Val"**: The value of the appropriate field from the models.WeightModel instance.
- **"field.Ptr"**: A pointer to the value of "field.Val". This is useful when the field value has pointer receiver methods. This is only set if the "field" value can be addressed by reflect.

As well as these variables, the following functions are also provided:

| Signature                                                                                                   | Description                                                                                                                                                                                                                                                                                                                                                                                                                                       | Example                                                                                                                                                                                             |
|-------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| <code>abs(x int&#124;uint&#124;float&#124;time.Duration) int&#124;uint&#124;float&#124;time.Duration</code> | Returns the absolute value of `x`.                                                                                                                                                                                                                                                                                                                                                                                                                | `abs(duration("month"))`: Returns the absolute value of the `time.Duration` for a month. I.e. calls `time.Duration.Abs()`.                                                                          |
| <code>float(x int&#124;uint&#124;float&#124;time.Duration) float64</code>                                   | Returns the value `x` as a `float64`.                                                                                                                                                                                                                                                                                                                                                                                                             | `float(1)`: Returns the `float64` representation of the integer literal: `1`, which would be `1.0`.                                                                                                 |
| <code>int(x int&#124;uint&#124;float&#124;time.Duration) int64</code>                                       | Returns the value `x` as a `int64`.                                                                                                                                                                                                                                                                                                                                                                                                               | `int(1.0)`: Returns the `int64` representation of the floating-point literal: `1.0`, which would be `1`.                                                                                            |
| <code>scale_range(x, xMin, xMax, yMin, yMax float64) float64</code>                                         | Scales the given float `x`, that is between `xMin` and `xMax`, so that it is between `yMin` and `yMax`. Also supports inverse ranges, where `yMax` is less than the `yMin`.                                                                                                                                                                                                                                                                       | `scale_range(50.0, 0.0, 100.0, 0.0, 1.0)`: Scales the floating-point literal: `50.0`, that is between `0.0` and `100.0`, so that it is between `0.0` and `1.0`. This invocation would return `0.5`. |
| <code>clamp(x, max float64) float64</code>                                                                  | Clamps `x` to a maximum of `max`.                                                                                                                                                                                                                                                                                                                                                                                                                 | `clamp(1.1, 1.0)`: Clamps `1.1` to a maximum of `1.0`. This invocation would return `1.0`.                                                                                                          |
| <code>clamp_min_max(x, min, max float64) float64</code>                                                     | Clamps `x` to a minimum of `min`, and a maximum of `max`.                                                                                                                                                                                                                                                                                                                                                                                         | `clamp_min_max(-0.1, 0.0, 1.0)`: Clamps `-0.1` to a minimum of `0.0`, and a maximum of `1.0`. This invocation would return `0.0`.                                                                   |
| <code>now() time.Time</code>                                                                                | Returns the result of `time.Now().UTC()`.                                                                                                                                                                                                                                                                                                                                                                                                         | `now()`: Returns the time now in UTC.                                                                                                                                                               |
| <code>duration(x string&#124;time.Duration&#124;int&#124;int64) time.Duration</code>                        | If `x` is one of the following `string`s: `"nanosecond"`, `"microsecond"`, `"millisecond"`, `"second"`, `"minute"`, `"hour"`, `"day"`, `"week"`, or `"month"` then a `time.Duration` representing that duration will be returned. Otherwise, the `string` will be parsed into a `time.Duration` using `time.ParseDuration`. If `x` is not a `string` then it will be cast to an `int64` and converted to a `time.Duration` instance and returned. | `duration("month")`: Returns the equivalent of `time.Hour * 24 * 30`.                                                                                                                               |
| <code>boolMap(y, n any) map[bool]any</code>                                                                 | Constructs a `map[bool]any` where `y` will be bound to the `true` key, and `n` will be bound to the `false` key.                                                                                                                                                                                                                                                                                                                                  | `boolMap("yes", "no")[boolVar]`: Implements a simple yes-or-no switch for a boolean value.                                                                                                          |

**When `int`, `uint`, or `float` are written, this refers to all possible bit sizes.**

#### Storefront config

| Key          | Type                            | Description                                                                |
|--------------|---------------------------------|----------------------------------------------------------------------------|
| `storefront` | `models.Storefront` as `string` | The [`models.Storefront`](db/models/scrape.go) that this config applies to |
| `tags`       | `*TagConfig` as `object`        | The [tag config](#tag-config) for this Storefront                          |

##### Tag config

Configuration settings for tag scraping. This only really applies to the [`models.SteamStorefront`](db/models/scrape.go) `models.Storefront`.

| Key                 | Type                 | Description                                                                                                                                                                                                                                                                                                                          |
|---------------------|----------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `default_value`     | `float64`            | Default value for tags that are not included in the `values` map. The value of each tag for a [`models.Game`](db/models/game.go) (on the `models.SteamStorefront`) will be multiplied by the number of upvotes it has then accumulated. The average of this accumulated value will be used to calculate `models.Game.WeightedScore`. |
| `upvotes_threshold` | `float64`            | The threshold for the number of upvotes a tag should have (i.e. >=) to be included in the accumulated value of all the tags for a [`models.Game`](db/models/game.go). If this is not set (== 0), then all the tags will be added.                                                                                                    |
| `values`            | `map[string]float64` | Map of tag names to values. It is useful for soft "banning" tags that we don't want to see in a game.                                                                                                                                                                                                                                |

#### Scrape constants

The defaults for these values (shown [here](#configuration)), are based off of a Twitter developer account with [Elevated access](https://developer.twitter.com/en/docs/twitter-api/getting-started/about-twitter-api#v2-access-level).

| Key                                                | Type                        | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | You should use these as a default (when using a Twitter API account with Elevated access)  |
|----------------------------------------------------|-----------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------|
| `scout_timeout`                                    | `time.Duration` as `string` | Maximum duration that the [`Scout`](scout.go) procedure can be run for. If it runs longer than this it will be stopped using a panic. The `Scout` procedure should take no longer than 4 hours to run with the given default constants provided below.                                                                                                                                                                                                                               | `5h`                                                                                       |
| `transform_tweet_workers`                          | `int`                       | Number of `TransformWorker` that will be spun up in the [`DiscoveryBatch`](discovery.go).                                                                                                                                                                                                                                                                                                                                                                                            | `5`                                                                                        |
| `reddit_subreddit_scrape_workers`                  | `int`                       | Number of [`redditSubredditScraper`](discovery.go) workers to start in [`RedditDiscoveryPhase`](discovery.go).                                                                                                                                                                                                                                                                                                                                                                       | `5`                                                                                        |
| `reddit_discovery_batch_workers`                   | `int`                       | Number of [`redditDiscoveryBatchWorker`](discovery.go) to start in [`RedditDiscoveryPhase`](discovery.go).                                                                                                                                                                                                                                                                                                                                                                           | `5`                                                                                        |
| `reddit_binding_paginator_wait_time`               | `int`                       | The `waitTime` to supply to any [`api.Paginator`](api/paginator.go) created for the Reddit [`api.API`](api/api.go).                                                                                                                                                                                                                                                                                                                                                                  | `500ms`                                                                                    |
| `reddit_comments_per_post`                         | `int`                       | Number of comments to retrieve per-post in [`SubredditFetch`](discovery.go).                                                                                                                                                                                                                                                                                                                                                                                                         | `100`                                                                                      |
| `reddit_posts_per_subreddit`                       | `int`                       | The number of posts to process per-subreddit. If there are more posts than this in top for the current week then we will divide the number of total posts available with `reddit_posts_per_subreddit` and use this number as the number of posts to skip before processing another post.                                                                                                                                                                                             | `50`                                                                                       |
| `update_developer_workers`                         | `int`                       | Number of `updateDeveloperWorker` that will be spun up in the [Update phase](update.go).                                                                                                                                                                                                                                                                                                                                                                                             | `7`                                                                                        |
| `update_producer_finished_job_timeout`             | `time.Duration` as `string` | The amount of time that a producer within the [Update Phase](update.go) will wait after not seeing any new finished jobs.                                                                                                                                                                                                                                                                                                                                                            | `2s`                                                                                       |
| `update_producer_finished_job_max_timeouts`        | `int`                       | The number of timeouts that can occur within a producer in the [Update Phase](update.go) before dropping that batch of jobs. The total amount of time represented by this constant can be calculated using the following formula: `update_producer_finished_job_timeout * update_producer_finished_job_max_timeouts`.                                                                                                                                                                | `2700`: _2700 * 2s = 1h30m_                                                                |
| `max_update_tweets`                                | `int`                       | Maximum number of tweets fetched per [`models.Developer`](db/models/developer.go) in the [Update phase](update.go).                                                                                                                                                                                                                                                                                                                                                                  | `11`                                                                                       |
| `max_update_posts`                                 | `int`                       | Maximum number of Reddit posts fetched per [`models.Developer`](db/models/developer.go) in the [Update phase](update.go).                                                                                                                                                                                                                                                                                                                                                            | `11`                                                                                       |
| `reddit_producer_batch_multiplier`                 | `float64`                   | Multiplier that will be applied to the batch size (passed into the [Scout procedure](scout.go)) for the [`redditBatchProducer`](update.go) in the [Update Phase](update.go).                                                                                                                                                                                                                                                                                                         | `1.3`                                                                                      |
| `reddit_producer_batch_sleep_time`                 | `time.Duration` as `string` | Minimum amount of time to sleep after each queued batch of jobs the [`redditBatchProducer`](update.go) routine executes in the [Update Phase](update.go).                                                                                                                                                                                                                                                                                                                            | `5m`                                                                                       |
| `seconds_between_discovery_batches`                | `time.Duration` as `string` | Number of seconds to sleep between `DiscoveryBatch` batches.                                                                                                                                                                                                                                                                                                                                                                                                                         | `3s`                                                                                       |
| `seconds_between_update_batches`                   | `time.Duration` as `string` | Number of seconds to sleep between queue batches of `updateDeveloperJob`.                                                                                                                                                                                                                                                                                                                                                                                                            | `30s`                                                                                      |
| `max_total_discovery_tweets_daily_percent`         | `float64`                   | Maximum percentage that the `discoveryTweets` (parameter to the `Scout` procedure) number can be out of [`myTwitter.TweetsPerDay`](twitter/consts.go).                                                                                                                                                                                                                                                                                                                               | `0.55`                                                                                     |
| `max_enabled_developers_after_enable_phase`        | `float64`                   | Number of enabled [`models.Developer`s](db/models/developer.go) that should exist after the [Enable phase](enable.go) for each [`models.DeveloperType`](db/models/developer.go).                                                                                                                                                                                                                                                                                                     | `maxTotalUpdateTweets / maxUpdateTweets = 2250`                                            |
| `max_enabled_developers_after_disable_phase`       | `float64`                   | Number of [`models.Developer`s](db/models/developer.go) to keep in the [Disable phase](disable.go) for each [`models.DeveloperType`](db/models/developer.go).                                                                                                                                                                                                                                                                                                                        | `floor(maxEnabledDevelopersAfterEnablePhase * 0.9165) = 2062`                              |
| `max_developers_to_enable`                         | `float64`                   | Maximum number of [`models.Developer`s](db/models/developer.go) that can be re-enabled in the [Enable phase](enable.go) for each [`models.DeveloperType`](db/models/developer.go).                                                                                                                                                                                                                                                                                                   | `ciel(maxEnabledDevelopersAfterEnablePhase - maxEnabledDevelopersAfterDisablePhase) = 188` |
| `percentage_of_disabled_developers_to_delete`      | `float64`                   | Percentage of all disabled [`models.Developer`s](db/models/developer.go) to delete in the [Delete phase](delete.go).                                                                                                                                                                                                                                                                                                                                                                 | `0.025`                                                                                    |
| `stale_developer_days`                             | `int`                       | Number of days after which a developer can become stale if their latest snapshot was created `stale_developer_days` ago.                                                                                                                                                                                                                                                                                                                                                             | `70`                                                                                       |
| `discovery_game_scrape_workers`                    | `int`                       | Number of [`models.StorefrontScrapers`](db/models/scrape.go) to start in the [Discovery phase](discovery.go).                                                                                                                                                                                                                                                                                                                                                                        | `10`                                                                                       |
| `discovery_max_concurrent_game_scrape_workers`     | `int`                       | Number of [`models.StorefrontScrapers`](db/models/scrape.go) that can be processing a job at the same time in the [Discovery Phase](discovery.go).                                                                                                                                                                                                                                                                                                                                   | `9`                                                                                        |
| `update_game_scrape_workers`                       | `int`                       | Number of [`models.StorefrontScrapers`](db/models/scrape.go) to start in the [Update phase](enable.go).                                                                                                                                                                                                                                                                                                                                                                              | `10`                                                                                       |
| `update_max_concurrent_game_scrape_workers`        | `int`                       | Number of [`models.StorefrontScrapers`](db/models/scrape.go) that can be processing a job at the same time in the [Update Phase](update.go).                                                                                                                                                                                                                                                                                                                                         | `9`                                                                                        |
| `min_scrape_storefronts_for_game_worker_wait_time` | `time.Duration` as `string` | Minimum amount of time for a [`models.StorefrontScrapers`](db/models/scrape.go) to wait after completing a job in the [Discovery](discovery.go) and [Update](update.go) Phase.                                                                                                                                                                                                                                                                                                       | `100ms`                                                                                    |
| `max_scrape_storefronts_for_game_worker_wait_time` | `time.Duration` as `string` | Maximum amount of time for a [`models.StorefrontScrapers`](db/models/scrape.go) to wait after completing a job in the [Discovery](discovery.go) and [Update](update.go) Phase.                                                                                                                                                                                                                                                                                                       | `500ms`                                                                                    |
| `max_games_per_tweet`                              | `int`                       | Maximum number of games for each tweet we process in the [Discovery](discovery.go) and [Update](update.go) Phase. This is needed so that we don't overload the queue to the [`models.StorefrontScrapers`](db/models/scrape.go).                                                                                                                                                                                                                                                      | `10`                                                                                       |
| `max_games_per_post`                               | `int`                       | Maximum number of games for each Reddit post we process in the [Discovery](discovery.go) and [Update Phase](update.go). This is needed so that we don't overload the queue to the [`models.StorefrontScrapers`](db/models/scrape.go).                                                                                                                                                                                                                                                | `10`                                                                                       |
| `max_trend_workers`                                | `int`                       | Maximum number of `trendFinder` workers to start to find the [`models.Trend`](db/models/trend.go), [`models.DeveloperSnapshot`s](db/models/developer_snapshot.go), and [`models.Game`s](db/models/game.go) for a [`models.Developer`](db/models/developer.go) to use in the [`models.TrendingDev`](db/models/developer.go) field in [`email.MeasureContext`](email/contexts.go) for the [`email.Measure`](email/templates/measure.html) email [`email.Template`](email/template.go). | `10`                                                                                       |
| `max_trending_developers`                          | `int`                       | Maximum number of [`models.TrendingDev`](db/models/developer.go) to feature within the [`email.Measure`](email/templates/measure.html) email [`email.Template`](email/template.go).                                                                                                                                                                                                                                                                                                  | `10`                                                                                       |
| `max_top_steam_apps`                               | `int`                       | Maximum number of [`models.SteamApp`](db/models/steam_app.go) to feature within the [`email.Measure`](email/templates/measure.html) email [`email.Template`](email/template.go).                                                                                                                                                                                                                                                                                                     | `10`                                                                                       |
| `sock_steam_app_scrape_workers`                    | `int`                       | Number of [`models.StorefrontScrapers`](db/models/scrape.go) to start in the goroutine that will watch the output of the [`ScoutWebPipes` co-process](#steam-web-pipes).                                                                                                                                                                                                                                                                                                             | `5`                                                                                        |
| `sock_max_concurrent_steam_app_scrape_workers`     | `int`                       | Number of [`models.StorefrontScrapers`](db/models/scrape.go) that can be processing a job at the same time in the websocket client.                                                                                                                                                                                                                                                                                                                                                  | `4`                                                                                        |
| `sock_max_steam_app_scraper_jobs`                  | `int`                       | Maximum number of jobs that can be queued for the [`models.StorefrontScrapers`](db/models/scrape.go) instance in the websocket client.                                                                                                                                                                                                                                                                                                                                               | `2048`                                                                                     |
| `sock_steam_app_scraper_jobs_per_minute`           | `int`                       | Number of [`models.SteamApp`](db/models/steam_app.go) the websocket client should be completing every minute. If it falls below this threshold, and the number of jobs in the [`models.StorefrontScrapers`](db/models/scrape.go) queue is above `sock_steam_app_scraper_drop_jobs_threshold` then the dropper goroutine will drop some jobs the next time it is running.                                                                                                             | `15`                                                                                       |
| `sock_steam_app_scraper_drop_jobs_threshold`       | `int`                       | Threshold of jobs in the [`models.StorefrontScrapers`](db/models/scrape.go) at which the dropper goroutine will start to drop jobs to make room.                                                                                                                                                                                                                                                                                                                                     | `sockMaxSteamAppScraperJobs / 4 = 512`                                                     |
| `sock_min_steam_app_scraper_wait_time`             | `time.Duration` as `string` | Minimum amount of time for a [`models.StorefrontScrapers`](db/models/scrape.go) to wait after completing a job in the websocket client.                                                                                                                                                                                                                                                                                                                                              | `100ms`                                                                                    |
| `sock_max_steam_app_scraper_wait_time`             | `time.Duration` as `string` | Maximum amount of time for a [`models.StorefrontScrapers`](db/models/scrape.go) to wait after completing a job in the websocket client.                                                                                                                                                                                                                                                                                                                                              | `500ms`                                                                                    |
| `sock_steam_app_scrape_consumers`                  | `int`                       | Number of consumers to start in the websocket client to consume the scraped [`models.SteamApp`](db/models/steam_app.go) from the [`models.StorefrontScrapers`](db/models/scrape.go) instance.                                                                                                                                                                                                                                                                                        | `10`                                                                                       |
| `sock_dropper_wait_time`                           | `time.Duration` as `string` | Amount time for the dropper goroutine to wait each time it has been run.                                                                                                                                                                                                                                                                                                                                                                                                             | `1m`                                                                                       |
| `scrape_max_tries`                                 | `int`                       | Maximum number of tries that is passed to a [`models.GameModelStorefrontScraper`](db/models/scrape.go) from [`models.ScrapeStorefrontForGameModel`](db/models/scrape.go) in a [`models.StorefrontScrapers`](db/models/scrape.go) instance. This affects the number of times a certain scrape procedure will be retried for a certain [`models.Storefront`](db/models/scrape.go).                                                                                                     | `3`                                                                                        |
| `scrape_min_delay`                                 | `time.Duration` as `string` | Minimum wait time after an unsuccessful try that is passed to a [`models.GameModelStorefrontScraper`](db/models/scrape.go) from [`models.ScrapeStorefrontForGameModel`](db/models/scrape.go) in a [`models.StorefrontScrapers`](db/models/scrape.go) instance. This affects the wait time after an unsuccessful call to a scrape procedure for a certain [`models.Storefront`](db/models/scrape.go).                                                                                 | `2s`                                                                                       |

### Steam web pipes

This contains the settings for the Steam web pipes co-process. This is an additional process spawned off when running `go run . worker`/`game-scout worker` that will connect to the Steam PICS changelist network and publish any changelists that it captures to a websocket that will be scraped by a goroutine listening as a client on this websocket. It is used to determine newly updated Steam store pages by favouring apps with a lower number of total updates (see [`models.SteamApp`](db/models/steam_app.go)). The code for this co-process is taken from the [SteamWebPipes repository](https://github.com/xPaw/SteamWebPipes) by xPaw.

| Key                        | Type     | Description                                                                                                                                                                                               |
|----------------------------|----------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Disable`                  | `bool`   | Whether to disable the `ScoutWebPipes` co-process.                                                                                                                                                        |
| `BinaryLocation`           | `string` | The relative location to the built `ScoutWebPipes`/`SteamWebPipes` binary                                                                                                                                 |
| `Location`                 | `string` | The address on which to start the websocket                                                                                                                                                               |
| `DatabaseConnectionString` | `string` | This is only provided for completeness for the `SteamWebPipes` process and is not used as we keep track of any [`models.SteamApp`s](db/models/steam_app.go) that we scrape in game-scout's PostgreSQL DB. |
| `X509Certificate`          | `string` | The location to the X509 certification                                                                                                                                                                    |
