package main

import (
	"container/heap"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/api"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/game-scout/email"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/andygello555/game-scout/monday"
	"github.com/andygello555/gotils/v2/slices"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"math"
	"strconv"
	"time"
)

type trendFinderResult struct {
	err error
	*models.TrendingDev
}

type trendFinderResultHeap struct {
	reverse bool
	arr     []*trendFinderResult
}

func (h trendFinderResultHeap) Len() int { return len(h.arr) }

func (h trendFinderResultHeap) Less(i, j int) bool {
	if h.reverse {
		return h.arr[i].Trend.GetCoeffs()[1] < h.arr[j].Trend.GetCoeffs()[1]
	} else {
		return h.arr[i].Trend.GetCoeffs()[1] > h.arr[j].Trend.GetCoeffs()[1]
	}
}

func (h trendFinderResultHeap) Swap(i, j int) { h.arr[i], h.arr[j] = h.arr[j], h.arr[i] }

func (h *trendFinderResultHeap) Push(x any) { h.arr = append(h.arr, x.(*trendFinderResult)) }

func (h *trendFinderResultHeap) Pop() any {
	old := *h
	n := len(old.arr)
	x := old.arr[n-1]
	h.arr = old.arr[0 : n-1]
	return x
}

func newTrendFinderResultHeap(reverse bool) trendFinderResultHeap {
	return trendFinderResultHeap{
		reverse: reverse,
		arr:     make([]*trendFinderResult, 0),
	}
}

func trendFinder(jobs <-chan *models.Developer, results chan<- *trendFinderResult) {
	for job := range jobs {
		result := &trendFinderResult{}
		if result.TrendingDev, result.err = job.TrendingDev(db.DB); result.err != nil {
			result.err = errors.Wrapf(result.err, "trendFinder cannot create TrendingDev")
			results <- result
			continue
		}

		log.INFO.Printf("Developer %v has %d games", job, len(result.Games))
		log.INFO.Printf("Developer %v has %d snapshots", job, len(result.Snapshots))
		log.INFO.Printf("Found trend for Developer %v = %.2f", job, result.Trend.GetCoeffs()[1])
		results <- result
	}
}

func MeasurePhase(state *ScoutState) (err error) {
	// Before we do anything let us fetch the Games and SteamApps that are on the linked Monday board.
	var (
		mondayGames              []*models.Game
		mondaySteamApps          []*models.SteamApp
		gamePaginator            api.Paginator[monday.ItemResponse, []*models.Game]
		steamAppPaginator        api.Paginator[monday.ItemResponse, []*models.SteamApp]
		mondayGameIDs            mapset.Set[uuid.UUID]
		mondayWatchedGameIDs     mapset.Set[uuid.UUID]
		mondaySteamAppIDs        mapset.Set[uint64]
		mondayWatchedSteamAppIDs mapset.Set[uint64]
	)

	if globalConfig.Monday != nil {
		if gamePaginator, err = api.NewTypedPaginator(monday.DefaultClient, time.Millisecond*100, models.GetGamesFromMonday, globalConfig.Monday, db.DB); err != nil {
			log.ERROR.Printf("Could not create paginator for GetGamesFromMonday: %v", err)
		} else {
			if mondayGames, err = gamePaginator.All(); err != nil {
				log.ERROR.Printf("Could not find all Games using Paginator for GetGamesFromMonday: %v", err)
			}
		}

		if steamAppPaginator, err = api.NewTypedPaginator(monday.DefaultClient, time.Millisecond*100, models.GetSteamAppsFromMonday, globalConfig.Monday, db.DB); err != nil {
			log.ERROR.Printf("Could not create paginator for GetSteamAppsFromMonday: %v", err)
		} else {
			if mondaySteamApps, err = steamAppPaginator.All(); err != nil {
				log.ERROR.Printf("Could not find all SteamApps using Paginator for GetSteamAppsFromMonday: %v", err)
			}
		}

		log.INFO.Printf("We have found %d Games on the linked Monday board/group. Saving...", len(mondayGames))
		mondayGameIDs = mapset.NewSet(slices.Comprehension(mondayGames, func(idx int, value *models.Game, arr []*models.Game) uuid.UUID {
			return value.ID
		})...)
		mondayWatchedGameIDs = mapset.NewSet[uuid.UUID]()
		for _, game := range mondayGames {
			if game.MondayItemID != 0 && game.MondayBoardID != "" {
				var boardID int64
				if boardID, err = strconv.ParseInt(game.MondayBoardID, 10, 64); err != nil {
					log.ERROR.Printf(
						"\tCould not parse board ID %q for linked item %d for Game %q: %v",
						game.MondayBoardID, game.MondayItemID, game.String(), err,
					)
				} else {
					if _, err = models.UpdateGameInMonday.Execute(monday.DefaultClient, game, game.MondayItemID, int(boardID), globalConfig.Monday); err != nil {
						log.ERROR.Printf(
							"\tCould not update linked item %d for Game %q: %v",
							game.MondayItemID, game.String(), err,
						)
					}
				}
			}

			isWatchedLog := " "
			if game.Watched != nil {
				isWatchedLog = fmt.Sprintf(" is watched (%s) and has ", *game.Watched)
				mondayWatchedGameIDs.Add(game.ID)
			}
			log.INFO.Printf("\tGame %q%shas vote score: %d", game.String(), isWatchedLog, game.Votes)

			if err = db.DB.Save(game).Error; err != nil {
				log.ERROR.Printf("\tCould not save Game %q: %v", game.String(), err)
			}
		}

		log.INFO.Printf("We have found %d SteamApps on the linked Monday board/group. Saving...", len(mondaySteamApps))
		mondaySteamAppIDs = mapset.NewSet(slices.Comprehension(mondaySteamApps, func(idx int, value *models.SteamApp, arr []*models.SteamApp) uint64 {
			return value.ID
		})...)
		mondayWatchedSteamAppIDs = mapset.NewSet[uint64]()
		for _, app := range mondaySteamApps {
			if app.MondayItemID != 0 && app.MondayBoardID != "" {
				var boardID int64
				if boardID, err = strconv.ParseInt(app.MondayBoardID, 10, 64); err != nil {
					log.ERROR.Printf(
						"\tCould not parse board ID %q for linked item %d for SteamApp %q: %v",
						app.MondayBoardID, app.MondayItemID, app.String(), err,
					)
				} else {
					if _, err = models.UpdateSteamAppInMonday.Execute(monday.DefaultClient, app, app.MondayItemID, int(boardID), globalConfig.Monday); err != nil {
						log.ERROR.Printf(
							"\tCould not update linked item %d for SteamApp %q: %v",
							app.MondayItemID, app.String(), err,
						)
					}
				}
			}

			if app.Watched != nil {
				log.INFO.Printf("\tSteamApp %q is watched", app.String())
				mondayWatchedSteamAppIDs.Add(app.ID)
			}

			if err = db.DB.Save(app).Error; err != nil {
				log.ERROR.Printf("\tCould not save Game %q: %v", app.String(), err)
			}
		}
	} else {
		log.WARNING.Println("There is no Monday config, so we will skip checking votes and watched status for Games and SteamApps")
	}

	startAny, _ := state.GetCachedField(StateType).Get("Start")
	start := startAny.(time.Time)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	debugAny, _ := state.GetCachedField(StateType).Get("Debug")
	debug := debugAny.(bool)

	// If the weekday of the start of the scrape does not match the weekday that Measure emails are sent out on and
	// debug is not set, then the MeasurePhase does not need to be run.
	if !debug && globalConfig.Email.EmailTemplateConfigFor(email.Measure).TemplateSendDay() != start.Weekday() {
		log.WARNING.Printf(
			"Measure Phase does not need to be run as Scout was started on %s, which was/is not a %s",
			start.Format("Monday 2 January"),
			globalConfig.Email.EmailTemplateConfigFor(email.Measure).TemplateSendDay().String(),
		)
		return nil
	}

	measureContext := email.MeasureContext{
		Start:                  start.Add(time.Hour * 24 * 7 * -1),
		End:                    start,
		TrendingDevs:           make([]*models.TrendingDev, 0),
		DevelopersBeingDeleted: *state.GetIterableCachedField(DeletedDevelopersType).(*DeletedDevelopers),
		WatchedDevelopers:      make([]*models.TrendingDev, 0),
		WatchedSteamApps:       make([]*models.SteamApp, 0),
		Config:                 globalConfig.Email,
	}

	// To find the top performing Developers we need to:
	// 1. Get all non-disabled developers that have more than 1 snapshot to their name
	// 2. Find the regression line coefficients for each in a worker pattern
	// 3. Order the Developers by the values of these coefficients descending
	// 4: Slice off the top maxTrendingDevelopers
	// 5: Increment each developer's TimesHighlighted field
	// 6. Add the collected trending developers to the MeasureContext instance
	var enabledDevelopers []*models.Developer
	var enabledDevelopersNo int64
	// select developers.*
	// from developers
	// left join developer_snapshots on developers.id = developer_id
	// where not disabled
	// group by developers.id
	// having count(developer_snapshots.id) > 1;
	if err = db.DB.Select(
		"developers.*",
	).Joins(
		"LEFT JOIN developer_snapshots on developers.id = developer_id",
	).Where(
		"not disabled",
	).Group(
		"developers.id",
	).Having(
		"COUNT(developer_snapshots.id) > 1",
	).Find(&enabledDevelopers).Error; err != nil {
		return myErrors.TemporaryWrap(false, err, "cannot fetch enabled developers in Measure")
	}

	if err = db.DB.Model(&models.Developer{}).Where("not disabled").Count(&enabledDevelopersNo).Error; err != nil {
		log.ERROR.Printf("Could not find the number of enabled developers: %v", err)
	}
	log.INFO.Printf(
		"There are %d enabled developers, we are going to find the trend of %d developers",
		enabledDevelopersNo, len(enabledDevelopers),
	)

	jobs := make(chan *models.Developer, len(enabledDevelopers))
	results := make(chan *trendFinderResult, len(enabledDevelopers))
	for w := 0; w < globalConfig.Scrape.Constants.MaxTrendWorkers; w++ {
		go trendFinder(jobs, results)
	}

	for _, enabledDeveloper := range enabledDevelopers {
		jobs <- enabledDeveloper
	}
	close(jobs)

	topDevelopers := newTrendFinderResultHeap(false)
	for i := 0; i < len(enabledDevelopers); i++ {
		result := <-results
		if result.err != nil {
			log.ERROR.Printf(
				"Error occurred when trying to find trend for Developer %v: %v, skipping...",
				result.Developer, result.err,
			)
		} else {
			state.GetCachedField(StateType).SetOrAdd("Result", "MeasureStats", "SampledTrendingDevelopers", models.SetOrAddInc.Func())
			heap.Push(&topDevelopers, result)
		}
	}
	close(results)

	// Add the top maxTrendingDevelopers (or however many we found)
	topDevelopersNo := int(math.Min(float64(topDevelopers.Len()), float64(globalConfig.Scrape.Constants.MaxTrendingDevelopers)))
	log.INFO.Printf("Adding top %d developers to the MeasureContext", topDevelopersNo)
	for i := 0; i < topDevelopersNo; i++ {
		topDeveloper := heap.Pop(&topDevelopers).(*trendFinderResult)
		topDeveloper.SetPosition(i + 1)
		topDeveloper.SetOutOf(int(enabledDevelopersNo))
		log.INFO.Printf(
			"%d: Adding Developer %v with trend %.10f to email context...",
			i+1, topDeveloper.Developer, topDeveloper.Trend.GetCoeffs()[1],
		)

		if globalConfig.Monday != nil && len(topDeveloper.Games) > 0 {
			log.INFO.Printf(
				"Developer %v has tweeted about %d Games, adding these to linked Monday board/group",
				topDeveloper.Developer, len(topDeveloper.Games),
			)
			for _, game := range topDeveloper.Games {
				if !mondayGameIDs.Contains(game.ID) {
					if _, err = models.AddGameToMonday.Execute(monday.DefaultClient, game, globalConfig.Monday); err != nil {
						log.ERROR.Printf("\tCould not add Game %q to Monday: %v", game.String(), err)
					} else {
						log.INFO.Printf("\tAdded Game %q to Monday board/group", game.String())
					}
				} else {
					log.WARNING.Printf("\tGame %q already exists on the linked Monday board/group. Skipping...", game.String())
				}
			}
		}

		if !debug {
			// Update the number of times this developer has been highlighted
			if err = db.DB.Model(topDeveloper.Developer).Update("times_highlighted", gorm.Expr("times_highlighted + 1")).Error; err != nil {
				log.ERROR.Printf("Could not increment TimesHighlighted for Developer %v: %v", topDeveloper.Developer, err)
			}
		} else {
			log.WARNING.Printf(
				"ScoutState.Debug is set, so we are skipping incrementing times_highlighted on Developer %v",
				topDeveloper.Developer,
			)
		}
		measureContext.TrendingDevs = append(measureContext.TrendingDevs, topDeveloper.TrendingDev)
	}

	// To find the top performing SteamApps we just need to make a query for any SteamApps that are not Bare and don't
	// have null weighted scores. These are then ordered by weighted score descending and the query is limited to
	// maxTopSteamApps. We then add these SteamApps to the MeasureContext.
	var steamApps []*models.SteamApp
	if err = db.DB.Where(
		"not bare and weighted_score is not null",
	).Order(
		"weighted_score desc",
	).Limit(globalConfig.Scrape.Constants.MaxTopSteamApps).Find(&steamApps).Error; err != nil {
		return myErrors.TemporaryWrapf(
			false, err,
			"could not find the top %d SteamApps", globalConfig.Scrape.Constants.MaxTopSteamApps,
		)
	}
	measureContext.TopSteamApps = steamApps

	log.INFO.Printf("Updating times highlighted and adding %d SteamApps to Monday", len(steamApps))
	for _, steamApp := range steamApps {
		if !debug {
			if err = db.DB.Model(steamApp).Update("times_highlighted", gorm.Expr("times_highlighted + 1")).Error; err != nil {
				log.ERROR.Printf("\tCould not increment TimesHighlighted for %s (%d): %v", steamApp.Name, steamApp.ID, err)
			}
		} else {
			log.WARNING.Printf(
				"ScoutState.Debug is set, so we are skipping incrementing times_highlighted on SteamApp %q",
				steamApp.String(),
			)
		}

		if globalConfig.Monday != nil && !mondaySteamAppIDs.Contains(steamApp.ID) {
			if _, err = models.AddSteamAppToMonday.Execute(monday.DefaultClient, steamApp, globalConfig.Monday); err != nil {
				log.ERROR.Printf("\tCould not add SteamApp %q to Monday: %v", steamApp.String(), err)
			} else {
				log.INFO.Printf("\tAdded SteamApp %q to Monday board/group", steamApp.String())
			}
		} else {
			log.WARNING.Printf("\tSteamApp %q already exists on the linked Monday board/group. Skipping...", steamApp.String())
		}
	}

	// We then find fill out the fields in the MeasureContext pertaining to Watched SteamApps and Games
	if globalConfig.Monday != nil {
		var watchedGames []*models.Game
		if err = db.DB.Find(&watchedGames, "watched is not null").Error; err != nil {
			log.ERROR.Printf("Could not find Watched Games: %v", err)
		}
		log.INFO.Printf("There are %d Games that are marked as Watched in the DB", len(watchedGames))
		for _, watchedGame := range watchedGames {
			if !mondayWatchedGameIDs.Contains(watchedGame.ID) {
				log.WARNING.Printf("\tGame %q is no longer being watched on Monday. Updating...", watchedGame.String())
				if len(watchedGame.Developers) == 0 {
					log.WARNING.Printf(
						"\tGame %q is no longer being watched AND has no referenced \"developers\" so it will be deleted",
						watchedGame.String(),
					)
					if err = db.DB.Delete(watchedGame).Error; err != nil {
						log.ERROR.Printf(
							"\t\tCould not delete Game %q that is no longer being watched and has no referenced \"developers\"",
							watchedGame.String(),
						)
					}
				} else {
					watchedGame.Watched = nil
					if err = db.DB.Save(watchedGame).Error; err != nil {
						log.ERROR.Printf("\tCould not save Game %q after setting Watched to nil: %v", watchedGame.String(), err)
					}
				}
			} else {
				log.INFO.Printf("\tGame %q is still being watched on Monday getting TrendingDev", watchedGame.String())
				watchedTrendingDev := models.TrendingDev{
					Games: []*models.Game{watchedGame},
				}
				developer := models.Developer{}
				if err = db.DB.Limit(1).Find(&developer, "username IN ?", []string(watchedGame.VerifiedDeveloperUsernames)).Error; err != nil {
					log.WARNING.Printf("\tCould not find verified developer for Game %q: %v", watchedGame.String(), err)
				} else {
					log.INFO.Printf("\tFound verified Developer for Game %q: %v", watchedGame.String(), developer)
					watchedTrendingDev.Developer = &developer
					if watchedTrendingDev.Snapshots, err = developer.DeveloperSnapshots(db.DB); err != nil {
						log.WARNING.Printf(
							"\tCould not find snapshots for Developer %v, who is verified for Game %q: %v",
							developer, watchedGame.String(), err,
						)
					}

					if watchedTrendingDev.Trend, err = developer.Trend(db.DB); err != nil {
						log.WARNING.Printf(
							"\tCould not find trend for Developer %v, who is verified for Game %q: %v",
							developer, watchedGame.String(), err,
						)
					}
					measureContext.WatchedDevelopers = append(measureContext.WatchedDevelopers, &watchedTrendingDev)
				}
			}
		}

		var watchedSteamApps []*models.SteamApp
		if err = db.DB.Find(&watchedSteamApps, "watched is not null").Error; err != nil {
			log.ERROR.Printf("Could not find Watched SteamApps: %v", err)
		}
		log.INFO.Printf("There are %d SteamApps that are marked as Watched in the DB", len(watchedSteamApps))
		for _, watchedApp := range watchedSteamApps {
			if !mondayWatchedSteamAppIDs.Contains(watchedApp.ID) {
				log.WARNING.Printf("\tSteamApp %q is no longer being watched on Monday. Updating...", watchedApp.String())
				watchedApp.Watched = nil
				if err = db.DB.Save(watchedApp).Error; err != nil {
					log.ERROR.Printf("\tCould not save SteamApp %q after setting Watched to nil: %v", watchedApp.String(), err)
				}
			} else {
				log.INFO.Printf("\tSteamApp %q is still being watched on Monday adding to MeasureContext", watchedApp.String())
				measureContext.WatchedSteamApps = append(measureContext.WatchedSteamApps, watchedApp)
			}
		}
	}

	// Finally, we construct the email and send it
	var pdf *email.Template
	if pdf = measureContext.Execute().PDF(); pdf.Error != nil {
		return myErrors.TemporaryWrap(false, pdf.Error, "could not construct Measure PDF")
	}

	if err = pdf.WriteFile(fmt.Sprintf(
		"measure_email_for_%d_developers_%s.pdf",
		len(enabledDevelopers),
		time.Now().UTC().Format("2006-01-02"),
	)); err != nil {
		return err
	}

	log.INFO.Printf("Sending email...")
	var resp email.Response
	if resp = pdf.SendSync(); resp.Error != nil {
		log.ERROR.Printf("Could not send email: %v", err)
	}
	state.GetCachedField(StateType).SetOrAdd("Result", "MeasureStats", "EmailSendTimeTaken", resp.Email.Profiling.Total())
	state.GetCachedField(StateType).SetOrAdd("Result", "MeasureStats", "EmailSize", resp.Email.Size())
	log.INFO.Println(resp.Email.Profiling.String())
	return
}
