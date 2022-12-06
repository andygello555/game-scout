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

const (
	maxStorefrontScrapersWorkers           = 10
	maxConcurrentStorefrontScrapersWorkers = 9
	maxStorefrontScraperJobs               = 2048
	minStorefrontScraperWaitTime           = time.Millisecond * 100
	maxStorefrontScraperWaitTime           = time.Millisecond * 500
	gameScrapeConsumers                    = 10
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
Type: %s
`, wsm.Users, wsm.ChangeNumber, wsm.Apps, wsm.Packages, wsm.Type.String())
}

// steamAppWebsocketConsumer is the worker procedure that is used within the SteamAppWebsocketScraper client to consume
// the scraped models.SteamApp. Once it has received a SteamApp, it will try and find whether the models.Developer for
// the models.SteamApp exists in the DB to be linked to it. Finally, the models.SteamApp will be upserted into the DB.
func steamAppWebsocketConsumer(wg *sync.WaitGroup, jobs chan (<-chan models.GameModel[uint64])) {
	defer wg.Done()
	for job := range jobs {
		// Wait for the scrape to have finished...
		app := (<-job).(*models.SteamApp)

		if app.Developer != nil {
			// If the developer is set in the SteamApp, then we will search for the Developer in the DB to see if we can
			// link it to a developer that we are tracking
			if err := db.DB.Find(
				app.Developer,
				"username = ?",
				app.Developer.Username,
			).Error; err == nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				log.INFO.Printf("Found Twitter user %s (%s)")
				app.DeveloperID = &app.Developer.ID
			} else if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
				log.INFO.Printf(
					"Could not find Twitter user %s in DB for SteamApp %s (%d), so we are skipping linking this developer",
					app.Developer.Username, app.Name, app.ID,
				)
				app.DeveloperID = nil
				app.Developer = nil
			} else {
				log.ERROR.Printf(
					"Error occurred whilst trying to find Twitter user %s in DB for SteamApp %s (%d): %v",
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
	}
}

func SteamAppWebsocketScraper(c *websocket.Conn, wg *sync.WaitGroup, config models.ScrapeConfig) {
	gameScrapers := models.NewStorefrontScrapers[uint64](
		config, db.DB, maxStorefrontScrapersWorkers, maxConcurrentStorefrontScrapersWorkers,
		maxStorefrontScraperJobs, minStorefrontScraperWaitTime, maxStorefrontScraperWaitTime,
	)
	gameScrapers.Start()

	consumerJobs := make(chan (<-chan models.GameModel[uint64]))
	var consumerWg sync.WaitGroup
	for w := 0; w < gameScrapeConsumers; w++ {
		consumerWg.Add(1)
		go steamAppWebsocketConsumer(&consumerWg, consumerJobs)
	}

	defer gameScrapers.Wait()
	defer consumerWg.Wait()
	defer func() {
		close(consumerJobs)
	}()
	defer wg.Done()

	if err := models.CreateInitialSteamApps(db.DB); err != nil {
		panic(err)
	}

	for {
		var msg SteamAppWebsocketMessage
		err := c.ReadJSON(&msg)
		if err != nil {
			log.WARNING.Println("read:", err)
			return
		}

		log.INFO.Println(msg.String())
		if msg.Type == ChangelistType {
			for appID := range msg.Apps {
				var appIDInt int64
				if appIDInt, err = strconv.ParseInt(appID, 10, 64); err == nil {
					consumerJobs <- gameScrapers.Add(false, &models.SteamApp{}, map[models.Storefront]mapset.Set[uint64]{
						models.SteamStorefront: mapset.NewThreadUnsafeSet[uint64](uint64(appIDInt)),
					})
				}
			}
		}
	}
}
