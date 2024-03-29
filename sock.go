package main

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"strconv"
	"sync"
	"time"
)

type WebsocketMessageType string

const (
	ChangelistType  WebsocketMessageType = "Changelist"
	UsersOnlineType WebsocketMessageType = "UsersOnline"
)

func (wsmType WebsocketMessageType) String() string {
	return string(wsmType)
}

type SteamAppWebsocketMessage struct {
	ChangeNumber int                  `json:"ChangeNumber"`
	Apps         map[string]string    `json:"Apps"`
	Packages     map[string]string    `json:"Packages"`
	Users        int                  `json:"Users"`
	Type         WebsocketMessageType `json:"Type"`
}

func (wsm *SteamAppWebsocketMessage) String() string {
	return fmt.Sprintf(`Change no. %d
Users: %d
Apps: %v
Packages: %v
Type: %s`, wsm.Users, wsm.ChangeNumber, wsm.Apps, wsm.Packages, wsm.Type.String())
}

// steamAppWebsocketConsumer is the worker procedure that is used within the SteamAppWebsocketScraper client to consume
// the scraped models.SteamApp. Once it has received a SteamApp, it will try and find whether the models.Developer for
// the models.SteamApp exists in the DB to be linked to it. Finally, the models.SteamApp will be upserted into the DB.
func steamAppWebsocketConsumer(no int, wg *sync.WaitGroup, timeout time.Duration, jobs chan (<-chan models.GameModel[uint64])) {
	defer wg.Done()
	for job := range jobs {
		// Wait for the scrape to have finished
		gameModel := <-job
		if gameModel == nil {
			log.WARNING.Printf(
				"SteamAppWebsocketConsumer %d: has a nil SteamApp, most likely because it has been dropped or because "+
					"of a timeout. There are %d jobs left, %.2f%% capacity",
				no+1, len(jobs), float64(len(jobs))/float64(globalConfig.Scrape.Constants.SockMaxSteamAppScraperJobs)*100.0,
			)
			continue
		}

		app := gameModel.(*models.SteamApp)
		log.WARNING.Printf(
			"SteamAppWebsocketConsumer %d: is processing job for SteamApp \"%s\" (%d). "+
				"There are %d jobs left, %.2f%% capacity",
			no+1, app.Name, app.ID, len(jobs), float64(len(jobs))/float64(globalConfig.Scrape.Constants.SockMaxSteamAppScraperJobs)*100.0,
		)

		if app.Type == "game" {
			if app.Developer != nil {
				// If the developer is set in the SteamApp, then we will search for the Developer in the DB to see if we can
				// link it to a developer that we are tracking
				if err := db.DB.Find(
					app.Developer,
					"username = ?",
					app.Developer.Username,
				).Error; err == nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					log.INFO.Printf(
						"Found Twitter user \"%s\" (%s) for SteamApp \"%s\" (%d)",
						app.Developer.Username, app.Developer.ID, app.Name, app.ID,
					)
					if app.Developer.ID != "" {
						app.DeveloperID = &app.Developer.ID
					}
				} else if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
					log.INFO.Printf(
						"Could not find Twitter user \"%s\" in DB for SteamApp \"%s\" (%d), so we are skipping linking "+
							"this developer",
						app.Developer.Username, app.Name, app.ID,
					)
					app.DeveloperID = nil
					app.Developer = nil
				} else {
					log.ERROR.Printf(
						"Error occurred whilst trying to find Twitter user \"%s\" in DB for SteamApp \"%s\" (%d): %v",
						app.Developer.Username, app.Name, app.ID, err,
					)
					app.DeveloperID = nil
					app.Developer = nil
				}
			}

			// Finally, upsert the SteamApp
			if created, err := db.Upsert(app); err != nil {
				log.ERROR.Printf("Could not upsert SteamApp %d: %v", app.ID, err)
			} else {
				log.INFO.Printf("Scraped and upserted SteamApp %d: created = %t", app.ID, created)
			}
		} else {
			log.WARNING.Printf("SteamApp %d is not a Game, it is a \"%s\". Skipping creation...", app.ID, app.Type)
		}
	}
}

// SteamAppWebsocketScraper is the set of workers that will read changelists published by the ScoutWebPipes co-process
// from the websocket, scrape each app, and then save the scraped app as a models.SteamApp to the DB.
type SteamAppWebsocketScraper struct {
	workers int
	c       *websocket.Conn
	wg      *sync.WaitGroup
	close   chan struct{}
	config  *Config
}

// NewSteamAppWebsocketScraper creates a new SteamAppWebsocketScraper with the given number of workers and Config.
func NewSteamAppWebsocketScraper(workers int, config *Config) *SteamAppWebsocketScraper {
	return &SteamAppWebsocketScraper{
		workers: workers,
		c:       nil,
		wg:      &sync.WaitGroup{},
		close:   make(chan struct{}, workers),
		config:  config,
	}
}

// Start connects the SteamAppWebsocketScraper to the websocket as well as starting the workers for handling the
// changelists as they come in.
func (sws *SteamAppWebsocketScraper) Start() {
	// Start a websocket client that will listen to all the changelogs being pushed
	log.INFO.Printf("Websocket client is connecting to \"%s\"", sws.config.SteamWebPipes.Location)
	// Start the websocket and dial into the location that is stored in the SteamWebPipesConfig
	err := errors.New("")
	// We keep trying to dial in until we've not got an error
	for err != nil {
		if sws.c, _, err = websocket.DefaultDialer.Dial(sws.config.SteamWebPipes.Location, nil); err != nil {
			log.WARNING.Printf("Could not dial into \"%s\": %s, waiting 2s", sws.config.SteamWebPipes.Location, err.Error())
		}
		time.Sleep(time.Second * 2)
	}

	for w := 0; w < sws.workers; w++ {
		log.INFO.Printf("Starting SteamAppWebsocketScraper worker no. %d", w+1)
		sws.wg.Add(1)
		go sws.worker(w)
	}
}

// Stop will send a stop signal to each worker and then wait for
func (sws *SteamAppWebsocketScraper) Stop() error {
	for w := 0; w < sws.workers; w++ {
		log.WARNING.Printf("Sending stop signal to SteamAppWebsocketScraper worker no. %d", w+1)
		sws.close <- struct{}{}
	}
	log.WARNING.Printf("Waiting for SteamAppWebsocketScraper workers to finish...")
	sws.wg.Wait()
	log.WARNING.Printf("Closing SteamAppWebsocketScraper's websocket connection...")
	return errors.Wrapf(
		sws.c.Close(),
		"could not close websocket connection to \"%s\"",
		sws.config.SteamWebPipes.Location,
	)
}

// worker is spun up by Start, workers number of times. Each worker will read changelists from the shared websocket
// connection. If the changelist contains apps, then these apps will be scraped using a models.StorefrontScrapers
// instance to produce a scraped models.SteamApp. The done channel of the Add method of models.StorefrontScrapers will
// be sent to a batch of consumers that will save the scraped models.SteamApp to the DB.
func (sws *SteamAppWebsocketScraper) worker(no int) {
	gameScrapers := models.NewStorefrontScrapers[uint64](
		sws.config.Scrape, db.DB,
		globalConfig.Scrape.Constants.SockSteamAppScrapeWorkers,
		globalConfig.Scrape.Constants.SockMaxConcurrentSteamAppScrapeWorkers,
		globalConfig.Scrape.Constants.SockMaxSteamAppScraperJobs,
		globalConfig.Scrape.Constants.SockMinSteamAppScraperWaitTime.Duration,
		globalConfig.Scrape.Constants.SockMaxSteamAppScraperWaitTime.Duration,
	)
	gameScrapers.Start()

	consumerJobs := make(chan (<-chan models.GameModel[uint64]), globalConfig.Scrape.Constants.SockMaxSteamAppScraperJobs)
	var consumerWg sync.WaitGroup
	for w := 0; w < globalConfig.Scrape.Constants.SockSteamAppScrapeConsumers; w++ {
		consumerWg.Add(1)
		go steamAppWebsocketConsumer(w, &consumerWg, gameScrapers.Timeout, consumerJobs)
	}

	dropperClose := make(chan struct{})
	// Defer all the functions to stop this worker gracefully...
	defer func() {
		// Close the dropper before returning out of the worker
		log.WARNING.Printf("Stopping dropper goroutine for SteamAppWebsocketScraper worker no. %d", no+1)
		dropperClose <- struct{}{}

		log.WARNING.Printf("Waiting for SteamAppWebsocketScraper worker no. %d's StorefrontScraper to finish...", no+1)
		// We use the Stop method here as there could be 100+ jobs in the StorefrontScrapers queue
		gameScrapers.Stop()

		// Drop the last consumer jobs so that we don't have to timeout 2048 times in the consumers
		log.WARNING.Printf(
			"Dropping last %d consumer jobs for SteamAppWebsocketScraper worker no. %d",
			len(consumerJobs), no+1,
		)
	done:
		for {
			select {
			case <-consumerJobs:
			default:
				break done
			}
		}
		close(consumerJobs)

		// Wait for the consumers to finish
		log.WARNING.Printf("Waiting for SteamAppWebsocketScraper worker no. %d's consumers to finish", no+1)
		consumerWg.Wait()

		// Finally, mark this SteamAppWebsocketScraper worker as done
		log.WARNING.Printf("SteamAppWebsocketScraper worker no. %d is finished", no+1)
		sws.wg.Done()
	}()

	if err := models.CreateInitialSteamApps(db.DB); err != nil {
		panic(err)
	}

	// This goroutine will keep checking the number of jobs queued into the StorefrontScrapers every minute. If it's
	// falling below storefrontScraperJobsPerMinute and the number of enqueued jobs is greater than
	// storefrontScraperDropJobsThreshold, then we will start dropping jobs so that the rate at which jobs are going
	// through the pipeline exceeds storefrontScraperJobsPerMinute.
	go func() {
		previousJobLen := 0
		for {
			select {
			case <-dropperClose:
				log.WARNING.Printf("Stopped dropper for SteamAppWebsocketScraper worker no. %d", no+1)
				return
			case <-time.After(globalConfig.Scrape.Constants.SockDropperWaitTime.Duration):
				jobs := gameScrapers.Jobs()
				rate := previousJobLen - jobs
				if jobs > globalConfig.Scrape.Constants.SockSteamAppScraperDropJobsThreshold {
					// We clamp a negative rate to the negative of storefrontScraperJobsPerMinute. This is so that we
					// can deal with a sudden increase in the number of jobs, but we also won't drop every new job that
					// came in.
					if rate < -1*globalConfig.Scrape.Constants.SockSteamAppScraperJobsPerMinute {
						rate = -1 * globalConfig.Scrape.Constants.SockSteamAppScraperJobsPerMinute
					}

					log.WARNING.Printf(
						"Number of StorefrontScraper jobs exceeds %d with %d jobs, rate is %d",
						globalConfig.Scrape.Constants.SockSteamAppScraperDropJobsThreshold, jobs, rate,
					)
					if rate < globalConfig.Scrape.Constants.SockSteamAppScraperJobsPerMinute {
						dropped := gameScrapers.Drop(globalConfig.Scrape.Constants.SockSteamAppScraperJobsPerMinute - rate)
						log.WARNING.Printf(
							"StorefrontScraper: %d jobs per minute falls below %d jobs per minute. Dropped %d/%d jobs",
							rate, globalConfig.Scrape.Constants.SockSteamAppScraperJobsPerMinute, dropped, globalConfig.Scrape.Constants.SockSteamAppScraperJobsPerMinute-rate,
						)
						jobs -= dropped
					}
				}
				previousJobLen = jobs
				log.INFO.Printf(
					"Dropper: previous jobs = %d, rate = %d, sleeping for %s",
					previousJobLen, rate, globalConfig.Scrape.Constants.SockDropperWaitTime.String(),
				)
			}
		}
	}()

	for {
		select {
		case <-sws.close:
			return
		default:
			var msg SteamAppWebsocketMessage
			err := sws.c.ReadJSON(&msg)
			if err != nil {
				log.WARNING.Println("read:", err)
				return
			}

			if msg.Type == ChangelistType {
				i := 0
				for appID := range msg.Apps {
					var appIDInt int64
					if appIDInt, err = strconv.ParseInt(appID, 10, 64); err == nil {
						log.INFO.Printf(
							"SteamAppWebsocketScraper worker no. %d is sending app %d (%d/%d in the batch) to be "+
								"scraped. There are %d jobs left, %.2f%% capacity",
							no+1, appIDInt, i+1, len(msg.Apps),
							gameScrapers.Jobs(), float64(gameScrapers.Jobs())/float64(globalConfig.Scrape.Constants.SockMaxSteamAppScraperJobs)*100.0,
						)
						if gameChannel, ok := gameScrapers.Add(false, &models.SteamApp{}, map[models.Storefront]mapset.Set[uint64]{
							models.SteamStorefront: mapset.NewThreadUnsafeSet[uint64](uint64(appIDInt)),
						}); ok {
							consumerJobs <- gameChannel
						}
					}
					i++
				}
			}
		}
	}
}
