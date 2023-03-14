package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/RichardKnop/machinery/v1/backends/result"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/andygello555/game-scout/api"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/game-scout/email"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/andygello555/game-scout/monday"
	"github.com/andygello555/game-scout/reddit"
	"github.com/andygello555/game-scout/steamcmd"
	task "github.com/andygello555/game-scout/tasks"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/andygello555/gotils/v2/slices"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/urfave/cli"
	"github.com/volatiletech/null/v9"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
)

var (
	cliApp *cli.App
)

func init() {
	// Initialise a CLI app
	cliApp = cli.NewApp()
	cliApp.Name = "game-scout"
	cliApp.Usage = "the game-scout worker"
	cliApp.Version = "0.0.0"
	if err := LoadConfig(); err != nil {
		panic(err)
	}
}

func main() {
	rand.Seed(time.Now().Unix())

	// Compile any compilable configs in the globalConfig
	start := time.Now().UTC()
	log.INFO.Printf("Compiling config at %s", start.String())
	if err := globalConfig.Compile(); err != nil {
		panic(err)
	}
	log.INFO.Printf("Done compiling config in %s", time.Now().UTC().Sub(start).String())

	// We set up the global DB instance...
	start = time.Now().UTC()
	log.INFO.Printf("Setting up DB at %s", start.String())
	if err := db.Open(globalConfig.DB); err != nil {
		panic(err)
	}
	defer db.Close()
	log.INFO.Printf("Done setting up DB in %s", time.Now().UTC().Sub(start).String())

	// Set up the twitter API client
	start = time.Now().UTC()
	log.INFO.Printf("Setting up Twitter client at %s", start.String())
	if err := myTwitter.ClientCreate(globalConfig.Twitter); err != nil {
		panic(err)
	}
	log.INFO.Printf("Done setting up Twitter client in %s", time.Now().UTC().Sub(start).String())

	// Set up the default Monday client
	start = time.Now().UTC()
	log.INFO.Printf("Setting up Monday client at %s", start.String())
	monday.CreateClient(globalConfig.Monday)
	log.INFO.Printf("Done setting up Monday client in %s", time.Now().UTC().Sub(start).String())

	// Set up the default Reddit client
	start = time.Now().UTC()
	log.INFO.Printf("Setting up Reddit client at %s", start.String())
	reddit.CreateClient(globalConfig.Reddit)
	log.INFO.Printf("Done setting up Reddit client in %s", time.Now().UTC().Sub(start).String())

	// Set up the default email client
	start = time.Now().UTC()
	log.INFO.Printf("Setting up email client at %s", start.String())
	if err := email.ClientCreate(globalConfig.Email); err != nil {
		panic(err)
	}
	log.INFO.Printf("Done setting up email client in %s", time.Now().UTC().Sub(start).String())

	// Register any additional tasks for machinery
	start = time.Now().UTC()
	log.INFO.Printf("Registering additional tasks:")
	for i, t := range []struct {
		name string
		fun  any
	}{
		{"scout", Scout},
	} {
		log.INFO.Printf("\tRegistering task no. %d: \"%s\"", i+1, t.name)
		task.RegisterTask(t.name, t.fun)
	}
	log.INFO.Printf("Done setting up additional tasks in %s", time.Now().UTC().Sub(start).String())

	// Set the CLI app commands
	cliApp.Commands = []cli.Command{
		{
			Name:  "worker",
			Usage: "launch game-scout worker",
			Action: func(c *cli.Context) error {
				if err := worker(); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				return nil
			},
		},
		{
			Name:  "send",
			Usage: "send example tasks",
			Action: func(c *cli.Context) error {
				if !c.Args().Present() {
					if err := sendAll(); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					return nil
				} else {
					if err := sendOne(c.Args().First(), c.Args().Tail()...); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					return nil
				}
			},
		},
		{
			Name:  "tweetcap",
			Usage: "gets the Tweet cap from the twitter client",
			Action: func(c *cli.Context) error {
				fmt.Println(myTwitter.Client.TweetCap)
				return nil
			},
		},
		{
			Name:  "recentTweets",
			Usage: "gets the recent tweets for the hashtags in config.json",
			Action: func(c *cli.Context) (err error) {
				total := 10
				if c.Args().Present() {
					var total64 int64
					if total64, err = strconv.ParseInt(c.Args().First(), 10, 32); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					total = int(total64)
				}
				opts := twitter.TweetRecentSearchOpts{
					Expansions: []twitter.Expansion{
						twitter.ExpansionEntitiesMentionsUserName,
						twitter.ExpansionAuthorID,
						twitter.ExpansionReferencedTweetsID,
						twitter.ExpansionReferencedTweetsIDAuthorID,
						twitter.ExpansionInReplyToUserID,
					},
					TweetFields: []twitter.TweetField{
						twitter.TweetFieldCreatedAt,
						twitter.TweetFieldConversationID,
						twitter.TweetFieldAttachments,
						twitter.TweetFieldPublicMetrics,
						twitter.TweetFieldReferencedTweets,
						twitter.TweetFieldContextAnnotations,
						twitter.TweetFieldEntities,
						twitter.TweetFieldAuthorID,
					},
					UserFields: []twitter.UserField{
						twitter.UserFieldDescription,
						twitter.UserFieldEntities,
						twitter.UserFieldPublicMetrics,
						twitter.UserFieldVerified,
						twitter.UserFieldPinnedTweetID,
						twitter.UserFieldCreatedAt,
					},
					SortOrder: twitter.TweetSearchSortOrderRelevancy,
				}
				var response myTwitter.BindingResult
				query := globalConfig.Twitter.TwitterQuery()
				if response, err = myTwitter.Client.ExecuteBinding(myTwitter.RecentSearch, &myTwitter.BindingOptions{Total: total}, query, opts); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}

				var enc []byte
				if enc, err = json.MarshalIndent(response.Raw().(*twitter.TweetRaw).TweetDictionaries(), "", "    "); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				fmt.Println(string(enc))
				return nil
			},
		},
		{
			Name:  "userLookup",
			Usage: "looks up the users with the given IDs",
			Action: func(c *cli.Context) (err error) {
				if !c.Args().Present() {
					return cli.NewExitError("no lookup given", 1)
				}
				opts := twitter.UserLookupOpts{
					Expansions: []twitter.Expansion{
						twitter.ExpansionPinnedTweetID,
					},
					TweetFields: []twitter.TweetField{
						twitter.TweetFieldCreatedAt,
						twitter.TweetFieldConversationID,
						twitter.TweetFieldAttachments,
						twitter.TweetFieldPublicMetrics,
						twitter.TweetFieldReferencedTweets,
						twitter.TweetFieldContextAnnotations,
						twitter.TweetFieldEntities,
					},
					UserFields: []twitter.UserField{
						twitter.UserFieldDescription,
						twitter.UserFieldEntities,
						twitter.UserFieldPublicMetrics,
						twitter.UserFieldVerified,
						twitter.UserFieldPinnedTweetID,
						twitter.UserFieldCreatedAt,
					},
				}
				var response myTwitter.BindingResult
				ids := make([]string, 0, len(c.Args()))
				ids = append(ids, c.Args().First())
				ids = append(ids, c.Args().Tail()...)
				if response, err = myTwitter.Client.ExecuteBinding(myTwitter.UserRetrieve, nil, ids, opts); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}

				var enc []byte
				if enc, err = json.MarshalIndent(response.Raw().(*twitter.UserRaw).UserDictionaries(), "", "    "); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				fmt.Println(string(enc))
				return nil
			},
		},
		{
			Name:  "usernameLookup",
			Usage: "looks up the users with the given usernames",
			Action: func(c *cli.Context) (err error) {
				if !c.Args().Present() {
					return cli.NewExitError("no lookup given", 1)
				}
				opts := twitter.UserLookupOpts{
					Expansions: []twitter.Expansion{
						twitter.ExpansionPinnedTweetID,
					},
					TweetFields: []twitter.TweetField{
						twitter.TweetFieldCreatedAt,
						twitter.TweetFieldConversationID,
						twitter.TweetFieldAttachments,
						twitter.TweetFieldPublicMetrics,
						twitter.TweetFieldReferencedTweets,
						twitter.TweetFieldContextAnnotations,
						twitter.TweetFieldEntities,
					},
					UserFields: []twitter.UserField{
						twitter.UserFieldDescription,
						twitter.UserFieldEntities,
						twitter.UserFieldPublicMetrics,
						twitter.UserFieldVerified,
						twitter.UserFieldPinnedTweetID,
						twitter.UserFieldCreatedAt,
					},
				}
				var response myTwitter.BindingResult
				usernames := make([]string, 0, len(c.Args()))
				usernames = append(usernames, c.Args().First())
				usernames = append(usernames, c.Args().Tail()...)
				if response, err = myTwitter.Client.ExecuteBinding(myTwitter.UserNameRetrieve, nil, usernames, opts); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}

				var enc []byte
				if enc, err = json.MarshalIndent(response.Raw().(*twitter.UserRaw).UserDictionaries(), "", "    "); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				fmt.Println(string(enc))
				return nil
			},
		},
		{
			Name: "testDB",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:     "no-delete",
					Usage:    "whether to not delete the create Developers and DeveloperSnapshots",
					Required: false,
				},
			},
			Usage: "test insertion into DB",
			Action: func(c *cli.Context) (err error) {
				opts := twitter.TweetRecentSearchOpts{
					Expansions: []twitter.Expansion{
						twitter.ExpansionEntitiesMentionsUserName,
						twitter.ExpansionAuthorID,
						twitter.ExpansionReferencedTweetsID,
						twitter.ExpansionReferencedTweetsIDAuthorID,
						twitter.ExpansionInReplyToUserID,
					},
					TweetFields: []twitter.TweetField{
						twitter.TweetFieldCreatedAt,
						twitter.TweetFieldConversationID,
						twitter.TweetFieldAttachments,
						twitter.TweetFieldPublicMetrics,
						twitter.TweetFieldReferencedTweets,
						twitter.TweetFieldContextAnnotations,
						twitter.TweetFieldEntities,
						twitter.TweetFieldAuthorID,
					},
					UserFields: []twitter.UserField{
						twitter.UserFieldDescription,
						twitter.UserFieldEntities,
						twitter.UserFieldPublicMetrics,
						twitter.UserFieldVerified,
						twitter.UserFieldPinnedTweetID,
						twitter.UserFieldCreatedAt,
					},
					SortOrder: twitter.TweetSearchSortOrderRelevancy,
				}
				var response myTwitter.BindingResult
				query := globalConfig.Twitter.TwitterQuery()
				if response, err = myTwitter.Client.ExecuteBinding(myTwitter.RecentSearch, &myTwitter.BindingOptions{Total: 10}, query, opts); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}

				rand.Seed(time.Now().Unix())
				var i int
				developerIDs := make([]string, 10)
				developerSnapshotIDs := make([]uuid.UUID, 10)
				gameIDs := make([]uuid.UUID, 10)
				for _, tweet := range response.Raw().(*twitter.TweetRaw).TweetDictionaries() {
					var createdAt time.Time
					if createdAt, err = time.Parse(globalConfig.Twitter.CreatedAtFormat, tweet.Author.CreatedAt); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}

					developer := models.Developer{
						ID:             tweet.Author.ID,
						Name:           tweet.Author.Name,
						Username:       tweet.Author.UserName,
						Description:    tweet.Author.Description,
						ProfileCreated: createdAt,
						PublicMetrics:  tweet.Author.PublicMetrics,
					}
					// Create the developer first...
					if tx := db.DB.Create(&developer); tx.Error != nil {
						return cli.NewExitError(tx.Error.Error(), 1)
					}

					publishers := []null.String{
						null.StringFromPtr(nil),
						null.StringFrom("fake"),
					}
					totalReviews := int32(rand.Intn(5000))
					positiveReviews := int32(rand.Intn(int(totalReviews)))
					totalUpvotes := int32(rand.Intn(5000))
					totalDownvotes := int32(rand.Intn(5000))
					totalComments := int32(rand.Intn(3000))
					tagScore := float64(rand.Intn(6000))
					game := models.Game{
						Name:            null.StringFrom("fake"),
						Storefront:      models.SteamStorefront,
						Website:         null.StringFrom("https://store.steampowered.com/app/240"),
						Developers:      []string{developer.ID},
						Publisher:       publishers[rand.Intn(len(publishers))],
						TotalReviews:    null.Int32From(totalReviews),
						PositiveReviews: null.Int32From(positiveReviews),
						NegativeReviews: null.Int32From(totalReviews - positiveReviews),
						TotalUpvotes:    null.Int32From(totalUpvotes),
						TotalDownvotes:  null.Int32From(totalDownvotes),
						TotalComments:   null.Int32From(totalComments),
						TagScore:        null.Float64From(tagScore),
					}
					// Then the game...
					if tx := db.DB.Create(&game); tx.Error != nil {
						return cli.NewExitError(tx.Error.Error(), 1)
					}

					developerSnapshot := models.DeveloperSnapshot{
						DeveloperID:                  developer.ID,
						Tweets:                       1,
						TweetTimeRange:               models.NullDurationFromPtr(nil),
						AverageDurationBetweenTweets: models.NullDurationFromPtr(nil),
						TweetsPublicMetrics:          tweet.Tweet.PublicMetrics,
						UserPublicMetrics:            tweet.Author.PublicMetrics,
						ContextAnnotationSet:         myTwitter.NewContextAnnotationSet(tweet.Tweet.ContextAnnotations...),
					}
					// Then finally the snapshot
					if tx := db.DB.Create(&developerSnapshot); tx.Error != nil {
						return cli.NewExitError(tx.Error.Error(), 1)
					}

					// We add the IDs to their respective arrays so we can delete them later...
					developerIDs[i] = developerSnapshot.DeveloperID
					developerSnapshotIDs[i] = developerSnapshot.ID
					gameIDs[i] = game.ID

					var tweetBytes []byte
					if tweetBytes, err = json.MarshalIndent(tweet, "", "    "); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					fmt.Printf("\n\n%d: %s\nWeighted score: %f\n", i+1, string(tweetBytes), developerSnapshot.WeightedScore)
					i++
				}

				if !c.Bool("no-delete") {
					if tx := db.DB.Where("id IN ?", developerIDs).Delete(&models.Developer{}); tx.Error != nil {
						return cli.NewExitError(tx.Error.Error(), 1)
					}
					if tx := db.DB.Where("id IN ?", gameIDs).Delete(&models.Game{}); tx.Error != nil {
						return cli.NewExitError(tx.Error.Error(), 1)
					}
				}
				return
			},
		},
		{
			Name: "scrapeGame",
			Flags: []cli.Flag{
				cli.UintFlag{
					Name:  "appid",
					Usage: "the appid of the Steam game to scrape",
				},
				cli.StringFlag{
					Name:  "website",
					Usage: "the URL of this game's website",
				},
				cli.StringFlag{
					Name:  "model",
					Usage: "the model to use for the scrape of the Steam game. Accepted values: game, steamapp",
					Value: "game",
				},
				cli.BoolFlag{
					Name:  "upsert",
					Usage: "whether to upsert the Game/SteamApp after it has been scraped",
				},
				cli.BoolFlag{
					Name:  "print",
					Usage: "whether to print the Game/SteamApp after it has been scraped",
				},
			},
			Usage: "scrape the Steam game with the given appid",
			Action: func(c *cli.Context) (err error) {
				var gameUpsertable db.Upsertable
				switch strings.ToLower(c.String("model")) {
				case "game":
					var website string
					switch {
					case c.IsSet("appid"):
						website = browser.SteamAppPage.Fill(c.Uint("appid"))
					case c.IsSet("website"):
						website = c.String("website")
					default:
						return cli.NewExitError("neither the appid or website flag was given", 1)
					}

					storefrontMap := make(map[models.Storefront]mapset.Set[string])
					switch {
					case models.SteamStorefront.ScrapeURL().Match(website):
						storefrontMap[models.SteamStorefront] = mapset.NewThreadUnsafeSet[string](website)
					case models.ItchIOStorefront.ScrapeURL().Match(website):
						storefrontMap[models.ItchIOStorefront] = mapset.NewThreadUnsafeSet[string](website)
					default:
						return cli.NewExitError("website does not match schema for neither Steam or ItchIO storefronts", 1)
					}

					gameScrapers := models.NewStorefrontScrapers[string](globalConfig.Scrape, db.DB, 1, 1, 1, 0, 0)
					gameScrapers.Start()
					if gameChannel, ok := gameScrapers.Add(false, &models.Game{}, storefrontMap); ok {
						gameModel := <-gameChannel
						if gameModel != nil {
							game := gameModel.(*models.Game)
							gameUpsertable = game
							fmt.Printf("Game: %q, IsGame = %t\n", game, game.IsGame)
						}
					}
					gameScrapers.Stop()
				case "steamapp":
					gameScrapers := models.NewStorefrontScrapers[uint64](globalConfig.Scrape, db.DB, 1, 1, 1, 0, 0)
					gameScrapers.Start()
					if gameChannel, ok := gameScrapers.Add(false, &models.SteamApp{}, map[models.Storefront]mapset.Set[uint64]{
						models.SteamStorefront: mapset.NewThreadUnsafeSet[uint64](uint64(c.Uint("appid"))),
					}); ok {
						gameModel := <-gameChannel
						if gameModel != nil {
							app := gameModel.(*models.SteamApp)
							gameUpsertable = app
							fmt.Printf("SteamApp: %q, Type = %q\n", app, app.Type)
						}
					}
					gameScrapers.Stop()
				default:
					return cli.NewExitError(
						fmt.Sprintf(
							"model \"%s\" is not an accepted model name. Choices: game, steamapp",
							strings.ToLower(c.String("model")),
						),
						1,
					)
				}

				if c.Bool("upsert") {
					if _, err = db.Upsert(gameUpsertable); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
				}

				if c.Bool("print") {
					fmt.Printf("%#v\n", gameUpsertable)
				}
				return
			},
		},
		{
			Name: "scout",
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:  "tweets",
					Usage: "the number of total tweets to scrape",
					Value: 200,
				},
				cli.IntFlag{
					Name:  "batchSize",
					Usage: "the number of tweets in each batch of the Scout process",
					Value: 100,
				},
			},
			Usage: "run the Scout function for the given number of tweets",
			Action: func(c *cli.Context) (err error) {
				if err = Scout(c.Int("batchSize"), c.Int("tweets")); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				return
			},
		},
		{
			Name: "updateComputedFields",
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name:  "models",
					Usage: "models to update computed fields for",
					Value: &cli.StringSlice{},
				},
				cli.StringSliceFlag{
					Name:  "pks",
					Usage: "the primary-key values of the instances of the models to update the computed fields for",
					Value: &cli.StringSlice{},
				},
			},
			Usage: "run UpdateComputedFieldsForModels for the given models names",
			Action: func(c *cli.Context) (err error) {
				if err = db.UpdateComputedFieldsForModels(
					c.StringSlice("models"),
					slices.Comprehension(
						c.StringSlice("pks"),
						func(idx int, value string, arr []string) any { return value },
					),
				); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				return
			},
		},
		{
			Name: "phase",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:     "phase",
					Usage:    "the name of the phase to run",
					Required: true,
				},
				cli.IntFlag{
					Name:  "batchSize",
					Usage: "the number of tweets that each batch will process",
					Value: 100,
				},
				cli.IntFlag{
					Name:  "discoveryTweets",
					Usage: "the total number of tweets to process across all batches",
					Value: 200,
				},
				cli.BoolFlag{
					Name:  "stateForceCreate",
					Usage: "whether to forceCreate the ScoutState",
				},
				cli.BoolFlag{
					Name:  "cleanup",
					Usage: "cleanup the state cache after finishing successfully",
				},
			},
			Usage: "run a single phase",
			Action: func(c *cli.Context) (err error) {
				var state *ScoutState
				if state, err = StateLoadOrCreate(c.Bool("stateForceCreate")); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}

				if state.Loaded {
					log.WARNING.Printf(
						"Previous ScoutState \"%s\" was loaded from disk so we are ignoring batchSize and discoveryTweets parameters:",
						state.BaseDir(),
					)
					log.WARNING.Println(state.String())
				} else {
					// If no state has been loaded, then we'll assume that the previous run of Scout was successful and
					// set up ScoutState as usual
					state.GetCachedField(StateType).SetOrAdd("Phase", PhaseFromString(c.String("phase")))
					state.GetCachedField(StateType).SetOrAdd("BatchSize", c.Int("batchSize"))
					state.GetCachedField(StateType).SetOrAdd("DiscoveryTweets", c.Int("discoveryTweets"))
				}
				state.GetCachedField(StateType).SetOrAdd("Debug", globalConfig.Scrape.Debug)

				if err = PhaseFromString(c.String("phase")).Run(state); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}

				fmt.Println("userTweetTimes:", state.GetIterableCachedField(UserTweetTimesType))
				fmt.Println("developerSnapshots:", state.GetIterableCachedField(DeveloperSnapshotsType))
				fmt.Println("gameIDs:", state.GetIterableCachedField(GameIDsType))
				fmt.Println("deletedDevelopers:", state.GetIterableCachedField(DeletedDevelopersType))
				if c.Bool("cleanup") {
					state.Delete()
				}
				return
			},
		},
		{
			Name: "config",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "view",
					Usage: "view the entire config as JSON",
				},
				cli.BoolFlag{
					Name:  "viewGO",
					Usage: "view the entire config as a GO struct",
				},
			},
			Usage: "subcommand for viewing/manipulating the currently loaded config.json",
			Action: func(c *cli.Context) (err error) {
				if c.Bool("view") {
					var jsonData []byte
					if jsonData, err = globalConfig.ToJSON(); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					fmt.Println(string(jsonData))
				}
				if c.Bool("viewGO") {
					fmt.Printf("%+v\n", globalConfig)
				}
				return
			},
		},
		{
			Name: "state",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "forceCreate",
					Usage: "whether to forceCreate the ScoutState",
				},
			},
			Usage: "view the most recent ScoutState or create a new ScoutState",
			Action: func(c *cli.Context) (err error) {
				var state *ScoutState
				if state, err = StateLoadOrCreate(c.Bool("forceCreate")); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				fmt.Println(state.String())
				return
			},
		},
		{
			Name: "email",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:     "subject",
					Required: true,
				},
				cli.StringFlag{
					Name:     "body",
					Required: true,
				},
				cli.StringFlag{
					Name:      "file",
					TakesFile: true,
				},
				cli.StringSliceFlag{
					Name:  "recipients",
					Value: &cli.StringSlice{},
				},
			},
			Action: func(c *cli.Context) (err error) {
				var e *email.Email
				if e, err = email.NewEmail(); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				e.Recipients = c.StringSlice("recipients")
				e.Subject = c.String("subject")
				e.FromName = globalConfig.Email.FromName
				e.FromAddress = globalConfig.Email.From
				defer func(e *email.Email) {
					err = myErrors.MergeErrors(err, e.Close())
				}(e)

				fileBytes, _ := os.ReadFile(c.String("file"))

				if err = e.AddParts(
					email.Part{Buffer: bytes.NewReader([]byte(c.String("body")))},
					email.Part{
						Buffer:     bytes.NewReader(fileBytes),
						Attachment: true,
						Filename:   filepath.Base(c.String("file")),
					},
				); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}

				if err = e.Write(); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}

				if err = email.Client.Send(e); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				return
			},
		},
		{
			Name:  "template",
			Usage: "test template creation and sending",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:     "template",
					Usage:    "the template to fill",
					Required: true,
				},
				cli.BoolFlag{
					Name:  "send",
					Usage: "send the email to its required recipients",
				},
				cli.BoolFlag{
					Name:  "loadState",
					Usage: "load the most recent state from disk",
				},
			},
			Action: func(c *cli.Context) (err error) {
				templatePath := email.TemplatePathFromName(c.String("template"))
				state := StateInMemory()
				if c.Bool("loadState") {
					if state, err = StateLoadOrCreate(false); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
				}

				var context email.Context
				switch templatePath {
				case email.Measure:
					context = &email.MeasureContext{
						Start: time.Now(),
						End:   time.Now(),
					}
				case email.Started:
					context = &StartedContext{state}
				case email.Error:
					stackTraces := new(strings.Builder)
					context = &ErrorContext{
						Time:        time.Now(),
						Error:       myErrors.TemporaryError(false, "this is a made-up error"),
						State:       state,
						StackTraces: stackTraces,
					}
					if err = pprof.Lookup("goroutine").WriteTo(stackTraces, 1); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
				case email.Finished:
					context = &email.FinishedContext{
						BatchSize:       100,
						DiscoveryTweets: 30250,
						Started:         time.Now().Add(-1 * time.Hour * 3),
						Finished:        time.Now(),
						Result: &models.ScoutResult{
							DiscoveryStats: &models.DiscoveryUpdateSnapshotStats{
								Developers:       rand.Int63(),
								Games:            rand.Int63(),
								TweetsConsumed:   rand.Int63(),
								TotalSnapshots:   rand.Int63(),
								SnapshotsCreated: rand.Int63(),
							},
							UpdateStats: &models.DiscoveryUpdateSnapshotStats{
								Developers:       rand.Int63(),
								Games:            rand.Int63(),
								TweetsConsumed:   rand.Int63(),
								TotalSnapshots:   rand.Int63(),
								SnapshotsCreated: rand.Int63(),
							},
							SnapshotStats: &models.DiscoveryUpdateSnapshotStats{
								Developers:       rand.Int63(),
								Games:            rand.Int63(),
								TweetsConsumed:   rand.Int63(),
								TotalSnapshots:   rand.Int63(),
								SnapshotsCreated: rand.Int63(),
							},
							DisableStats: &models.DisableEnableDeleteStats{
								EnabledDevelopersBefore:  rand.Int63(),
								DisabledDevelopersBefore: rand.Int63(),
								EnabledDevelopersAfter:   rand.Int63(),
								DisabledDevelopersAfter:  rand.Int63(),
								DeletedDevelopers:        rand.Int63(),
								TotalSampledDevelopers:   rand.Int63(),
							},
							EnableStats: &models.DisableEnableDeleteStats{
								EnabledDevelopersBefore:  rand.Int63(),
								DisabledDevelopersBefore: rand.Int63(),
								EnabledDevelopersAfter:   rand.Int63(),
								DisabledDevelopersAfter:  rand.Int63(),
								DeletedDevelopers:        rand.Int63(),
								TotalSampledDevelopers:   rand.Int63(),
							},
							DeleteStats: &models.DisableEnableDeleteStats{
								EnabledDevelopersBefore:  rand.Int63(),
								DisabledDevelopersBefore: rand.Int63(),
								EnabledDevelopersAfter:   rand.Int63(),
								DisabledDevelopersAfter:  rand.Int63(),
								DeletedDevelopers:        rand.Int63(),
								TotalSampledDevelopers:   rand.Int63(),
							},
							MeasureStats: &models.MeasureStats{
								SampledTrendingDevelopers: rand.Int63(),
								EmailSendTimeTaken:        time.Second * 30,
								EmailSize:                 rand.Int63(),
							},
						},
					}
				}

				var executed *email.Template
				if executed = context.Execute(); executed.Error != nil {
					return cli.NewExitError(err.Error(), 1)
				}

				if c.Bool("send") {
					if resp := executed.SendSync(); resp.Error != nil {
						return cli.NewExitError(err.Error(), 1)
					} else {
						fmt.Println(resp.Email.Profiling.String())
					}
				}
				return
			},
		},
		{
			Name: "developer",
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name:     "id",
					Usage:    "the IDs of the developers/Twitter users in the DB",
					Required: true,
				},
				cli.BoolFlag{
					Name:     "printBefore",
					Usage:    "print the developer before any of the subcommands have been run",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "games",
					Usage:    "view the games for this developer",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "update",
					Usage:    "update the developer using the UpdateDeveloper procedure",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "trend",
					Usage:    "find the trend of a Developer's snapshots",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "deletedDevelopers",
					Usage:    "add these Developers to the DeletedDevelopers cached field and save the cache",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "measure",
					Usage:    "execute the Measure template for this Developer",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "measureWatchedDevelopers",
					Usage:    "execute the Measure template for this Developer but treat it as though it's Games are being watched",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "measureWatchedGames",
					Usage:    "execute the Measure template for this Developer but treat it as though it's Games are being watched, and that it doesn't exist",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "measureDelete",
					Usage:    "execute the Measure template for this Developer but treat it as though it is being deleted",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "measureSendEmail",
					Usage:    "whether to send the email to the recipients of the Measure template defined in the TemplateConfig",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "printAfter",
					Usage:    "print the developer after all the subcommands have been run",
					Required: false,
				},
				cli.BoolFlag{
					Name:  "stateForceCreate",
					Usage: "whether to forceCreate the ScoutState",
				},
				cli.BoolFlag{
					Name:     "stateDelete",
					Usage:    "whether to delete the state when the developer command has finished",
					Required: false,
				},
			},
			Usage: "subcommand for viewing resources related to a developer in the DB",
			Action: func(c *cli.Context) (err error) {
				measureContext := email.MeasureContext{
					Start:                  time.Now(),
					End:                    time.Now(),
					TrendingDevs:           make([]*models.TrendingDev, len(c.StringSlice("id"))),
					DevelopersBeingDeleted: make([]*models.TrendingDev, 0),
					WatchedDevelopers:      make([]*models.TrendingDev, 0),
					Config:                 globalConfig.Email,
				}

				var state *ScoutState
				createState := c.Bool("update") || c.Bool("deletedDevelopers")
				if createState {
					if state, err = StateLoadOrCreate(c.Bool("stateForceCreate")); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
				}

				for developerNo, id := range c.StringSlice("id") {
					fmt.Printf("Performing commands on Developer: %s\n", id)
					var developer models.Developer
					if err = db.DB.Find(&developer, "id = ?", id).Error; err != nil {
						return cli.NewExitError(err.Error(), 1)
					}

					if c.Bool("printBefore") {
						fmt.Printf("\t%v\n", developer)
					}

					if c.Bool("games") {
						var games []*models.Game
						if games, err = developer.Games(db.DB); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}

						fmt.Printf("\tDeveloper %v, has %d games:\n", developer, len(games))
						if len(games) > 0 {
							for i, game := range games {
								fmt.Printf("\t\t%d) %v\n", i+1, game)
							}
						}
					}

					if c.Bool("update") {
						gameScrapers := models.NewStorefrontScrapers[string](
							globalConfig.Scrape, db.DB, 1, 1,
							12*globalConfig.Scrape.Constants.MaxGamesPerTweet,
							globalConfig.Scrape.Constants.MinScrapeStorefrontsForGameWorkerWaitTime.Duration,
							globalConfig.Scrape.Constants.MaxScrapeStorefrontsForGameWorkerWaitTime.Duration,
						)
						gameScrapers.Start()

						var gameIDs mapset.Set[uuid.UUID]
						if gameIDs, err = UpdateDeveloper(1, &developer, 12, gameScrapers, state); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
						gameScrapers.Wait()
						fmt.Println("\tuserTweetTimes:", state.GetIterableCachedField(UserTweetTimesType))
						fmt.Println("\tdeveloperSnapshots:", state.GetIterableCachedField(DeveloperSnapshotsType))
						fmt.Println("\tgameIDs:", gameIDs)
					}

					if c.Bool("trend") {
						var trend *models.Trend
						if trend, err = developer.Trend(db.DB); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}

						var chartBuffer *models.ChartImage
						if chartBuffer, err = trend.Chart(
							globalConfig.Email.EmailTemplateConfigFor(email.Measure).TemplateMaxImageWidth(),
							globalConfig.Email.EmailTemplateConfigFor(email.Measure).TemplateMaxImageHeight(),
						); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}

						if err = os.WriteFile(
							fmt.Sprintf(
								"developer_trend_%s_%s.png",
								developer.ID,
								time.Now().UTC().Format("2006-01-02"),
							),
							chartBuffer.Bytes(),
							filePerms,
						); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
						fmt.Printf("\tCoefficients for the trend of Developer %v: %v\n", developer, trend.GetCoeffs())
					}

					if c.Bool("measure") || c.Bool("measureWatchedDevelopers") || c.Bool("measureWatchedGames") || c.Bool("measureDelete") || c.Bool("deletedDevelopers") {
						if measureContext.TrendingDevs[developerNo], err = developer.TrendingDev(db.DB); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
						measureContext.TrendingDevs[developerNo].SetPosition(developerNo + 1)
						measureContext.TrendingDevs[developerNo].SetOutOf(len(c.StringSlice("id")))

						if c.Bool("measureWatchedDevelopers") {
							measureContext.WatchedDevelopers = append(measureContext.WatchedDevelopers, measureContext.TrendingDevs[developerNo])
						}

						if c.Bool("measureWatchedGames") {
							for _, game := range measureContext.TrendingDevs[developerNo].Games {
								measureContext.WatchedDevelopers = append(measureContext.WatchedDevelopers, &models.TrendingDev{
									Games: []*models.Game{game},
								})
							}
						}

						// Also add the TrendingDev to the DevelopersBeingDeleted slice if measureDelete is given
						if c.Bool("measureDelete") {
							measureContext.DevelopersBeingDeleted = append(measureContext.DevelopersBeingDeleted, measureContext.TrendingDevs[developerNo])
						}

						if c.Bool("deletedDevelopers") {
							fmt.Println("adding to deleted developers")
							state.GetIterableCachedField(DeletedDevelopersType).SetOrAdd(measureContext.TrendingDevs[developerNo])
							if err = state.Save(); err != nil {
								return cli.NewExitError(err.Error(), 1)
							}
						}
					}

					if c.Bool("printAfter") {
						fmt.Printf("\t%v\n", developer)
					}
				}

				if c.Bool("measure") {
					var template *email.Template
					if template = measureContext.Execute(); template.Error != nil {
						return cli.NewExitError(template.Error.Error(), 1)
					}

					filename := fmt.Sprintf("measure_email_for_%s_%s",
						strings.Join(c.StringSlice("id"), "_"),
						time.Now().UTC().Format("2006-01-02"),
					)

					if err = template.WriteFile(filename + ".html"); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}

					if template = template.PDF(); template.Error != nil {
						return cli.NewExitError(template.Error.Error(), 1)
					}

					if err = template.WriteFile(filename + ".pdf"); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}

					if c.Bool("measureSendEmail") {
						var resp email.Response
						if resp = template.SendSync(); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
						fmt.Println(resp.Email.Profiling.String())
					}
				}

				if createState && c.Bool("stateDelete") {
					state.Delete()
				}
				return
			},
		},
		{
			Name:  "monday",
			Usage: "run a Monday Client Binding",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "binding",
					Usage: "the name of the binding to execute",
				},
				cli.StringSliceFlag{
					Name:  "arg",
					Usage: "an arg to execute the binding with",
					Value: &cli.StringSlice{},
				},
				cli.BoolFlag{
					Name:  "all",
					Usage: "fetch all the resources from the binding using a typed Paginator",
				},
				cli.BoolFlag{
					Name:  "allAll",
					Usage: "fetch all the resources from the binding using a generic Paginator",
				},
				cli.BoolFlag{
					Name:  "log",
					Usage: "whether to enable logging for the Monday API Client",
				},
			},
			Action: func(c *cli.Context) (err error) {
				if c.Bool("log") {
					monday.DefaultClient.(*monday.Client).Log = func(s string) {
						log.INFO.Println(s)
					}
				}

				argsToInts := func(idx int, value string, arr []string) any {
					var intVal int64
					if intVal, err = strconv.ParseInt(value, 10, 64); err != nil {
						panic(err)
					}
					return int(intVal)
				}

				var execute func() (any, error)
				switch strings.ToLower(c.String("binding")) {
				case "getusers":
					execute = func() (any, error) {
						return monday.GetUsers.Execute(monday.DefaultClient)
					}
				case "getboards":
					args := slices.Comprehension(c.StringSlice("arg"), argsToInts)
					execute = func() (any, error) {
						return monday.GetBoards.Execute(monday.DefaultClient, args...)
					}
					if c.Bool("all") || c.Bool("allAll") {
						if len(args) > 0 {
							args = args[1:]
						}
						execute = func() (any, error) {
							if c.Bool("all") {
								if paginator, err := api.NewTypedPaginator(monday.DefaultClient, time.Millisecond*100, monday.GetBoards, args...); err != nil {
									return nil, err
								} else {
									all, err := paginator.All()
									if err == nil {
										fmt.Println("paginator items:", len(all))
									}
									return all, err
								}
							}

							if paginator, err := api.NewPaginator(monday.DefaultClient, time.Millisecond*100, api.WrapBinding(monday.GetBoards), args...); err != nil {
								return nil, err
							} else {
								all, err := paginator.All()
								if err == nil {
									fmt.Println("paginator items:", reflect.ValueOf(all).Len())
								}
								return all, err
							}
						}
					}
				case "getgroups":
					execute = func() (any, error) {
						return monday.GetGroups.Execute(monday.DefaultClient, slices.Comprehension(c.StringSlice("arg"), argsToInts)...)
					}
				case "getcolumns":
					execute = func() (any, error) {
						return monday.GetColumns.Execute(monday.DefaultClient, slices.Comprehension(c.StringSlice("arg"), argsToInts)...)
					}
				case "getcolumnmap":
					execute = func() (any, error) {
						return monday.GetColumnMap.Execute(monday.DefaultClient, slices.Comprehension(c.StringSlice("arg"), argsToInts)...)
					}
				case "getitems":
					args := slices.Comprehension[string, any](c.StringSlice("arg"), func(idx int, value string, arr []string) any {
						switch idx {
						case 0:
							var intVal int64
							if intVal, err = strconv.ParseInt(value, 10, 64); err != nil {
								panic(err)
							}
							return int(intVal)
						case 1:
							var intArr []int
							if err = json.Unmarshal([]byte(value), &intArr); err != nil {
								panic(err)
							}
							return intArr
						case 2:
							var stringArr []string
							if err = json.Unmarshal([]byte(value), &stringArr); err != nil {
								panic(err)
							}
							return stringArr
						default:
							panic(fmt.Errorf("getitems takes 3 arguments: int, []int, and []string"))
						}
					})
					execute = func() (any, error) {
						return monday.GetItems.Execute(monday.DefaultClient, args...)
					}
					if c.Bool("all") || c.Bool("allAll") {
						execute = func() (any, error) {
							if len(args) > 0 {
								args = args[1:]
							}
							if c.Bool("all") {
								if paginator, err := api.NewTypedPaginator(monday.DefaultClient, time.Millisecond*100, monday.GetItems, args...); err != nil {
									return nil, err
								} else {
									all, err := paginator.All()
									if err == nil {
										fmt.Println("paginator items:", len(all))
									}
									return all, err
								}
							}
							if paginator, err := api.NewPaginator(monday.DefaultClient, time.Millisecond*100, api.WrapBinding(monday.GetItems), args...); err != nil {
								return nil, err
							} else {
								all, err := paginator.All()
								if err == nil {
									fmt.Println("paginator items:", reflect.ValueOf(all).Len())
								}
								return all, err
							}
						}
					}
				case "addgame":
					args := c.StringSlice("arg")
					switch strings.ToLower(args[0]) {
					case "games":
						game := models.Game{}
						var id uuid.UUID
						if id, err = uuid.Parse(args[1]); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}

						if err = db.DB.Find(&game, id).Error; err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
						execute = func() (any, error) {
							return models.AddGameToMonday.Execute(monday.DefaultClient, &game, globalConfig.Monday)
						}
					case "steam_apps":
						app := models.SteamApp{}
						var id int64
						if id, err = strconv.ParseInt(args[1], 10, 64); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}

						if err = db.DB.Find(&app, id).Error; err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
						execute = func() (any, error) {
							return models.AddSteamAppToMonday.Execute(monday.DefaultClient, &app, globalConfig.Monday)
						}
					default:
						return cli.NewExitError("no model of name "+args[0], 1)
					}
				case "getgames":
					args := slices.Comprehension(c.StringSlice("arg")[1:], argsToInts)
					page := 0
					if len(args) > 0 {
						page = args[0].(int)
					}

					switch strings.ToLower(c.StringSlice("arg")[0]) {
					case "games":
						execute = func() (any, error) {
							return models.GetGamesFromMonday.Execute(monday.DefaultClient, page, globalConfig.Monday, db.DB)
						}
						if c.Bool("all") || c.Bool("allAll") {
							execute = func() (any, error) {
								if c.Bool("all") {
									if paginator, err := api.NewTypedPaginator(monday.DefaultClient, time.Millisecond*100, models.GetGamesFromMonday, globalConfig.Monday, db.DB); err != nil {
										return nil, err
									} else {
										all, err := paginator.All()
										if err == nil {
											fmt.Println("paginator items:", len(all))
										}
										return all, err
									}
								}
								if paginator, err := api.NewPaginator(monday.DefaultClient, time.Millisecond*100, api.WrapBinding(models.GetGamesFromMonday), globalConfig.Monday, db.DB); err != nil {
									return nil, err
								} else {
									all, err := paginator.All()
									if err == nil {
										fmt.Println("paginator items:", reflect.ValueOf(all).Len())
									}
									return all, err
								}
							}
						}
					case "steam_apps":
						execute = func() (any, error) {
							return models.GetSteamAppsFromMonday.Execute(monday.DefaultClient, page, globalConfig.Monday, db.DB)
						}
						if c.Bool("all") || c.Bool("allAll") {
							execute = func() (any, error) {
								if c.Bool("all") {
									if paginator, err := api.NewTypedPaginator(monday.DefaultClient, time.Millisecond*100, models.GetSteamAppsFromMonday, globalConfig.Monday, db.DB); err != nil {
										return nil, err
									} else {
										all, err := paginator.All()
										if err == nil {
											fmt.Println("paginator items:", len(all))
										}
										return all, err
									}
								}
								if paginator, err := api.NewPaginator(monday.DefaultClient, time.Millisecond*100, api.WrapBinding(models.GetSteamAppsFromMonday), globalConfig.Monday, db.DB); err != nil {
									return nil, err
								} else {
									all, err := paginator.All()
									if err == nil {
										fmt.Println("paginator items:", reflect.ValueOf(all).Len())
									}
									return all, err
								}
							}
						}
					default:
						return cli.NewExitError("no model of name "+c.StringSlice("arg")[0], 1)
					}
				case "updategame":
					args := c.StringSlice("arg")

					var itemID int64
					if itemID, err = strconv.ParseInt(args[2], 10, 64); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}

					var boardID int64
					if boardID, err = strconv.ParseInt(args[3], 10, 64); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}

					switch strings.ToLower(args[0]) {
					case "games":
						game := models.Game{}
						var id uuid.UUID
						if id, err = uuid.Parse(args[1]); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}

						if err = db.DB.Find(&game, id).Error; err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
						execute = func() (any, error) {
							return models.UpdateGameInMonday.Execute(monday.DefaultClient, &game, int(itemID), int(boardID), globalConfig.Monday)
						}
					case "steam_apps":
						app := models.SteamApp{}
						var id int64
						if id, err = strconv.ParseInt(args[1], 10, 64); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}

						if err = db.DB.Find(&app, id).Error; err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
						execute = func() (any, error) {
							return models.UpdateSteamAppInMonday.Execute(monday.DefaultClient, &app, int(itemID), int(boardID), globalConfig.Monday)
						}
					default:
						return cli.NewExitError("no model of name "+args[0], 1)
					}
				}

				var resource any
				if resource, err = execute(); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				fmt.Printf("%+v\n", resource)
				return
			},
		},
		{
			Name:  "reddit",
			Usage: "run a Reddit Client Binding",
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name:  "binding",
					Usage: "the name of the bindings to execute",
					Value: &cli.StringSlice{},
				},
				cli.StringSliceFlag{
					Name:  "arg",
					Usage: "an arg to execute the binding with",
					Value: &cli.StringSlice{},
				},
				cli.IntFlag{
					Name:     "pages",
					Required: false,
					Usage:    "fetch the given number of pages from the binding using a Paginator",
				},
			},
			Action: func(c *cli.Context) (err error) {
				for _, binding := range c.StringSlice("binding") {
					bindingName := strings.ToLower(binding)

					var (
						response any
						args     []any
					)
					bindingWrapper, _ := reddit.API.Binding(bindingName)
					if args, err = bindingWrapper.ArgsFromStrings(c.StringSlice("arg")...); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}

					if c.Int("pages") > 0 {
						var paginator api.Paginator[any, any]
						if paginator, err = bindingWrapper.Paginator(reddit.API.Client, time.Millisecond*500, args...); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
						if response, err = paginator.Pages(c.Int("pages")); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
						fmt.Println("paginator items:", reflect.ValueOf(response).Len())
					} else {
						if response, err = bindingWrapper.Execute(reddit.API.Client, args...); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
					}
					fmt.Printf("%v(%v): %+v\n", bindingWrapper, args, response)
				}
				return
			},
		},
		{
			Name:  "steamapp",
			Usage: "subcommand for viewing resources related to a SteamApp in the DB",
			Flags: []cli.Flag{
				cli.IntSliceFlag{
					Name:     "id",
					Usage:    "the App ID of a SteamApp in the DB",
					Required: true,
				},
				cli.BoolFlag{
					Name:     "printBefore",
					Usage:    "print the SteamApp before any of the flags have been run",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "update",
					Usage:    "update the SteamApp using the Update method",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "measure",
					Usage:    "execute the Measure template for this SteamApp",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "measureWatched",
					Usage:    "execute the Measure template for this SteamApp but treat it as though it has been Watched",
					Required: false,
				},
				cli.BoolFlag{
					Name:     "printAfter",
					Usage:    "print the SteamApp after all the flags have been run",
					Required: false,
				},
			},
			Action: func(c *cli.Context) (err error) {
				measureContext := email.MeasureContext{
					Start:            time.Now(),
					End:              time.Now(),
					TopSteamApps:     make([]*models.SteamApp, len(c.IntSlice("id"))),
					WatchedSteamApps: make([]*models.SteamApp, len(c.IntSlice("id"))),
					Config:           globalConfig.Email,
				}

				for steamAppNo, id := range c.IntSlice("id") {
					fmt.Printf("Performing commands on SteamApp: %d\n", id)
					var steamApp models.SteamApp
					if err = db.DB.Find(&steamApp, id).Error; err != nil {
						return cli.NewExitError(err.Error(), 1)
					}

					if c.Bool("printBefore") {
						fmt.Printf("\t%v\n", steamApp)
					}

					if c.Bool("update") {
						if err = steamApp.Update(db.DB, globalConfig.Scrape); err != nil {
							return cli.NewExitError(err.Error(), 1)
						}
					}

					if c.Bool("measure") {
						measureContext.TopSteamApps[steamAppNo] = &steamApp
					}

					if c.Bool("measureWatched") {
						measureContext.WatchedSteamApps[steamAppNo] = &steamApp
					}

					if c.Bool("printAfter") {
						fmt.Printf("\t%v\n", steamApp)
					}
				}

				if c.Bool("measure") {
					var template *email.Template
					if template = measureContext.Execute(); template.Error != nil {
						return cli.NewExitError(template.Error.Error(), 1)
					}

					filename := fmt.Sprintf(
						"measure_email_for_steamapps_%s_%s",
						strings.Join(
							slices.Comprehension(
								c.IntSlice("id"),
								func(idx int, value int, arr []int) string {
									return strconv.Itoa(value)
								},
							),
							"_",
						),
						time.Now().UTC().Format("2006-01-02"),
					)

					if err = template.WriteFile(filename + ".html"); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}

					if template = template.PDF(); template.Error != nil {
						return cli.NewExitError(template.Error.Error(), 1)
					}

					if err = template.WriteFile(filename + ".pdf"); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
				}
				return
			},
		},
		{
			Name:  "steamcmd",
			Usage: "runs a flow of commands in the SteamCMD wrapper",
			Action: func(c *cli.Context) (err error) {
				commands := make([]*steamcmd.CommandWithArgs, 0)
				for _, arg := range c.Args() {
					var cType steamcmd.CommandType
					if cType, err = steamcmd.CommandTypeFromString(arg); err == nil {
						commands = append(commands, steamcmd.NewCommandWithArgs(cType))
					} else {
						// Otherwise, we'll interpret the arg as an arg for the previous command
						if len(commands) > 0 {
							argValue, _ := steamcmd.ParseArgType(arg)
							commands[len(commands)-1].Args = append(commands[len(commands)-1].Args, argValue)
						} else {
							return cli.NewExitError("first argument is not a CommandType", 1)
						}
					}
				}

				cmd := steamcmd.New(true)
				if err = cmd.Flow(commands...); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}

				// Print outputs
				for i, output := range cmd.ParsedOutputs {
					fmt.Printf("Output %d:\n", i+1)
					switch output.(type) {
					case map[string]any:
						jsonData, _ := json.MarshalIndent(output, "", "  ")
						fmt.Println(string(jsonData))
					default:
						fmt.Printf("%v\n", output)
					}
					fmt.Println()
				}
				return
			},
		},
	}

	// Run the CLI app
	err := cliApp.Run(os.Args)
	if err != nil {
		return
	}
}

func sendOne(taskName string, args ...string) error {
	var err error
	var broker *task.Broker
	if broker, err = task.NewBroker(globalConfig.Tasks); err != nil {
		return err
	}
	defer broker.Cleanup()

	// Parse the arguments to a list of tasks.Args
	sigArgs := make([]tasks.Arg, len(args))
	for i, arg := range args {
		var parsed any
		var ok bool
		var typeName string
		var convert func(parse string) (any, bool)
		for typeName, convert = range map[string]func(parse string) (any, bool){
			"float64": func(parse string) (any, bool) {
				if value, err := strconv.ParseFloat(parse, 64); err != nil {
					return nil, false
				} else {
					return value, true
				}
			},
			"int64": func(parse string) (any, bool) {
				if value, err := strconv.ParseInt(parse, 10, 64); err != nil {
					return nil, false
				} else {
					return value, true
				}
			},
			"string": func(parse string) (any, bool) {
				return parse, true
			},
		} {
			if parsed, ok = convert(arg); ok {
				break
			}
		}
		sigArgs[i] = tasks.Arg{
			Value: parsed,
			Type:  typeName,
		}
	}

	// Construct the signature
	signature := tasks.Signature{
		Name: taskName,
		Args: sigArgs,
	}

	var asyncResult *result.AsyncResult
	asyncResult, err = broker.SendTaskWithContext(&signature)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	var results []reflect.Value
	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("Results: %v\n", tasks.HumanReadableResults(results))
	return nil
}

func sendAll() error {
	var (
		addTask0, addTask1, addTask2                      tasks.Signature
		multiplyTask0, multiplyTask1                      tasks.Signature
		sumIntsTask, sumFloatsTask, concatTask, splitTask tasks.Signature
		panicTask                                         tasks.Signature
		longRunningTask                                   tasks.Signature
	)

	var initTasks = func() {
		addTask0 = tasks.Signature{
			Name: "add",
			Args: []tasks.Arg{
				{
					Type:  "int64",
					Value: 1,
				},
				{
					Type:  "int64",
					Value: 1,
				},
			},
		}

		addTask1 = tasks.Signature{
			Name: "add",
			Args: []tasks.Arg{
				{
					Type:  "int64",
					Value: 2,
				},
				{
					Type:  "int64",
					Value: 2,
				},
			},
		}

		addTask2 = tasks.Signature{
			Name: "add",
			Args: []tasks.Arg{
				{
					Type:  "int64",
					Value: 5,
				},
				{
					Type:  "int64",
					Value: 6,
				},
			},
		}

		multiplyTask0 = tasks.Signature{
			Name: "multiply",
			Args: []tasks.Arg{
				{
					Type:  "int64",
					Value: 4,
				},
			},
		}

		multiplyTask1 = tasks.Signature{
			Name: "multiply",
		}

		sumIntsTask = tasks.Signature{
			Name: "sum_ints",
			Args: []tasks.Arg{
				{
					Type:  "[]int64",
					Value: []int64{1, 2},
				},
			},
		}

		sumFloatsTask = tasks.Signature{
			Name: "sum_floats",
			Args: []tasks.Arg{
				{
					Type:  "[]float64",
					Value: []float64{1.5, 2.7},
				},
			},
		}

		concatTask = tasks.Signature{
			Name: "concat",
			Args: []tasks.Arg{
				{
					Type:  "[]string",
					Value: []string{"foo", "bar"},
				},
			},
		}

		splitTask = tasks.Signature{
			Name: "split",
			Args: []tasks.Arg{
				{
					Type:  "string",
					Value: "foo",
				},
			},
		}

		panicTask = tasks.Signature{
			Name: "panic_task",
		}

		longRunningTask = tasks.Signature{
			Name: "long_running_task",
		}
	}

	var err error
	var broker *task.Broker
	if broker, err = task.NewBroker(globalConfig.Tasks); err != nil {
		return err
	}
	defer broker.Cleanup()

	log.INFO.Println("Starting batch:", broker.BatchID)
	/*
	* First, let's try sending a single task
	 */
	initTasks()

	log.INFO.Println("Single task:")

	asyncResult, err := broker.SendTaskWithContext(&addTask0)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err := asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("1 + 1 = %v\n", tasks.HumanReadableResults(results))

	/*
	* Try couple of tasks with a slice argument and slice return value
	 */
	asyncResult, err = broker.SendTaskWithContext(&sumIntsTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("sum([1, 2]) = %v\n", tasks.HumanReadableResults(results))

	asyncResult, err = broker.SendTaskWithContext(&sumFloatsTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("sum([1.5, 2.7]) = %v\n", tasks.HumanReadableResults(results))

	asyncResult, err = broker.SendTaskWithContext(&concatTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("concat([\"foo\", \"bar\"]) = %v\n", tasks.HumanReadableResults(results))

	asyncResult, err = broker.SendTaskWithContext(&splitTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("split([\"foo\"]) = %v\n", tasks.HumanReadableResults(results))

	/*
	* Now let's explore ways of sending multiple tasks
	 */

	// Now let's try a parallel execution
	initTasks()
	log.INFO.Println("Group of tasks (parallel execution):")

	group, err := tasks.NewGroup(&addTask0, &addTask1, &addTask2)
	if err != nil {
		return fmt.Errorf("Error creating group: %s", err.Error())
	}

	asyncResults, err := broker.SendGroupWithContext(group, 10)
	if err != nil {
		return fmt.Errorf("Could not send group: %s", err.Error())
	}

	for _, asyncResult := range asyncResults {
		results, err = asyncResult.Get(time.Millisecond * 5)
		if err != nil {
			return fmt.Errorf("Getting task result failed with error: %s", err.Error())
		}
		log.INFO.Printf(
			"%v + %v = %v\n",
			asyncResult.Signature.Args[0].Value,
			asyncResult.Signature.Args[1].Value,
			tasks.HumanReadableResults(results),
		)
	}

	// Now let's try a group with a chord
	initTasks()
	log.INFO.Println("Group of tasks with a callback (chord):")

	group, err = tasks.NewGroup(&addTask0, &addTask1, &addTask2)
	if err != nil {
		return fmt.Errorf("Error creating group: %s", err.Error())
	}

	chord, err := tasks.NewChord(group, &multiplyTask1)
	if err != nil {
		return fmt.Errorf("Error creating chord: %s", err)
	}

	chordAsyncResult, err := broker.SendChordWithContext(chord, 10)
	if err != nil {
		return fmt.Errorf("Could not send chord: %s", err.Error())
	}

	results, err = chordAsyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting chord result failed with error: %s", err.Error())
	}
	log.INFO.Printf("(1 + 1) * (2 + 2) * (5 + 6) = %v\n", tasks.HumanReadableResults(results))

	// Now let's try chaining task results
	initTasks()
	log.INFO.Println("Chain of tasks:")

	chain, err := tasks.NewChain(&addTask0, &addTask1, &addTask2, &multiplyTask0)
	if err != nil {
		return fmt.Errorf("Error creating chain: %s", err)
	}

	chainAsyncResult, err := broker.SendChainWithContext(chain)
	if err != nil {
		return fmt.Errorf("Could not send chain: %s", err.Error())
	}

	results, err = chainAsyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting chain result failed with error: %s", err.Error())
	}
	log.INFO.Printf("(((1 + 1) + (2 + 2)) + (5 + 6)) * 4 = %v\n", tasks.HumanReadableResults(results))

	// Let's try a task which throws panic to make sure stack trace is not lost
	initTasks()
	asyncResult, err = broker.SendTaskWithContext(&panicTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	_, err = asyncResult.Get(time.Millisecond * 5)
	if err == nil {
		return errors.New("Error should not be nil if task panicked")
	}
	log.INFO.Printf("Task panicked and returned error = %v\n", err.Error())

	// Let's try a long running task
	initTasks()
	asyncResult, err = broker.SendTaskWithContext(&longRunningTask)
	if err != nil {
		return fmt.Errorf("Could not send task: %s", err.Error())
	}

	results, err = asyncResult.Get(time.Millisecond * 5)
	if err != nil {
		return fmt.Errorf("Getting long running task result failed with error: %s", err.Error())
	}
	log.INFO.Printf("Long running task returned = %v\n", tasks.HumanReadableResults(results))

	return nil
}
