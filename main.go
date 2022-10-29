package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/RichardKnop/machinery/v1/backends/result"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	task "github.com/andygello555/game-scout/tasks"
	myTwitter "github.com/andygello555/game-scout/twitter"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/google/uuid"
	"github.com/urfave/cli"
	"github.com/volatiletech/null/v9"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"time"
)

var (
	app *cli.App
)

func init() {
	// Initialise a CLI app
	app = cli.NewApp()
	app.Name = "game-scout"
	app.Usage = "the game-scout worker"
	app.Version = "0.0.0"
	if err := LoadConfig(); err != nil {
		panic(err)
	}
}

func main() {
	// We set up the global DB instance...
	start := time.Now().UTC()
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

	// Set the CLI app commands
	app.Commands = []cli.Command{
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
			Description: "test insertion into DB",
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
					if createdAt, err = time.Parse(myTwitter.CreatedAtFormat, tweet.Author.CreatedAt); err != nil {
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
					game := models.Game{
						Name:            null.StringFrom("fake"),
						Storefront:      models.SteamStorefront,
						Website:         null.StringFrom("https://store.steampowered.com/app/240"),
						DeveloperID:     developer.ID,
						Publisher:       publishers[rand.Intn(len(publishers))],
						TotalReviews:    null.Int32From(totalReviews),
						PositiveReviews: null.Int32From(positiveReviews),
						NegativeReviews: null.Int32From(totalReviews - positiveReviews),
						TotalUpvotes:    null.Int32From(totalUpvotes),
						TotalDownvotes:  null.Int32From(totalDownvotes),
						TotalComments:   null.Int32From(totalComments),
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
			Name: "aggregateCommunityPosts",
			Flags: []cli.Flag{
				cli.Int64Flag{
					Name:  "appid",
					Usage: "the appid of the game to aggregate community posts for",
					Value: 477160,
				},
			},
			Description: "gets the total upvotes, downvotes, and comments for all community posts for a game",
			Action: func(c *cli.Context) (err error) {
				const batchSize = 50
				appID := c.Int64("appid")
				totalUpvotes := 0
				totalDownvotes := 0
				totalComments := 0
				gidEvent := "0"
				gidAnnouncement := "0"
				gids := mapset.NewSet[string]()

				for {
					var jsonBody map[string]any
					if jsonBody, err = browser.SteamCommunityPosts.JSON(appID, 0, batchSize, gidEvent, gidAnnouncement); err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					eventsProcessed := 0

					for _, event := range jsonBody["events"].([]interface{}) {
						eventBody := event.(map[string]any)
						gid := eventBody["gid"].(string)
						gidEvent = gid
						if !gids.Contains(gid) {
							gids.Add(gid)
							announcementBody := eventBody["announcement_body"].(map[string]any)
							gidAnnouncement = announcementBody["gid"].(string)
							totalUpvotes += int(announcementBody["voteupcount"].(float64))
							totalDownvotes += int(announcementBody["votedowncount"].(float64))
							totalComments += int(announcementBody["commentcount"].(float64))
							eventsProcessed++
						}
					}

					if eventsProcessed == 0 {
						break
					}
				}

				fmt.Println("Total upvotes:", totalUpvotes)
				fmt.Println("Total downvotes:", totalDownvotes)
				fmt.Println("Total comments:", totalComments)
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
			Description: "run the Scout function for the given number of tweets",
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
			},
			Description: "run UpdateComputedFieldsForModels for the given models names",
			Action: func(c *cli.Context) (err error) {
				if err = db.UpdateComputedFieldsForModels(c.StringSlice("models")...); err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				return
			},
		},
	}

	// Run the CLI app
	err := app.Run(os.Args)
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
