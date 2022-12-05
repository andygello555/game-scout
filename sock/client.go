package sock

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/gorilla/websocket"
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
		go func() {
			defer consumerWg.Done()
			for job := range consumerJobs {
				app := (<-job).(*models.SteamApp)
				if created, err := db.Upsert(app.Developer); err != nil {
					log.ERROR.Printf(
						"Could not upsert Developer %s (%s) for SteamApp %d",
						app.Developer.Username, app.Developer.ID, app.ID,
					)
				} else {
					log.INFO.Printf(
						"Upserted Developer %s (%s) for SteamApp %d: created = %t",
						app.Developer.Username, app.Developer.ID, created,
					)
				}
				app.Developer = nil

				if created, err := db.Upsert(app); err != nil {
					log.ERROR.Printf("Could not upsert SteamApp %d: %v", app.ID, err)
				} else {
					log.INFO.Printf("Scraped and upserted SteamApp %d: created = %t", app.ID, created)
				}
			}
		}()
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
