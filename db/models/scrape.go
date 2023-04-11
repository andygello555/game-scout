package models

import (
	"database/sql/driver"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/browser"
	urlfmt "github.com/andygello555/url-fmt"
	"github.com/antonmedv/expr/vm"
	mapset "github.com/deckarep/golang-set/v2"
	"gorm.io/gorm"
	"math/rand"
	"reflect"
	"sync"
	"time"
)

type Storefront string

const (
	UnknownStorefront Storefront = "U"
	SteamStorefront   Storefront = "S"
	ItchIOStorefront  Storefront = "I"
)

// Index returns the index of the Storefront.
func (sf Storefront) Index() int {
	switch sf {
	case SteamStorefront:
		return 1
	case ItchIOStorefront:
		return 2
	default:
		return 0
	}
}

// String returns the formal name of the Storefront.
func (sf Storefront) String() string {
	switch sf {
	case UnknownStorefront:
		return "Unknown"
	case SteamStorefront:
		return "Steam"
	case ItchIOStorefront:
		return "Itch.io"
	default:
		return "<nil>"
	}
}

// LogoSrc returns the URL at which the logo for this storefront resides.
func (sf Storefront) LogoSrc() string {
	switch sf {
	case SteamStorefront:
		return "https://store.cloudflare.steamstatic.com/public/shared/images/header/logo_steam.svg"
	case ItchIOStorefront:
		return "https://static.itch.io/images/logo-white-new.svg"
	default:
		return ""
	}
}

func (sf *Storefront) Scan(value interface{}) error {
	switch value.(type) {
	case []byte:
		*sf = Storefront(value.([]byte))
	case string:
		*sf = Storefront(value.(string))
	default:
		panic(fmt.Errorf("could not convert DB value to Storefront"))
	}
	return nil
}

func (sf Storefront) Value() (driver.Value, error) {
	return string(sf), nil
}

func (sf Storefront) Type() string {
	return "storefront_type"
}

func (sf Storefront) Values() []string {
	return []string{
		string(UnknownStorefront),
		string(SteamStorefront),
		string(ItchIOStorefront),
	}
}

// ScrapeURL returns the browser.ScrapeURL that can be used to return a standardised version of the URL for a game on
// this Storefront.
func (sf Storefront) ScrapeURL() urlfmt.URL {
	switch sf {
	case UnknownStorefront:
		return ""
	case SteamStorefront:
		return browser.SteamAppPage
	case ItchIOStorefront:
		return browser.ItchIOGamePage
	default:
		return ""
	}
}

func (sf Storefront) Storefronts() []Storefront {
	return []Storefront{
		UnknownStorefront,
		SteamStorefront,
		ItchIOStorefront,
	}
}

// TagConfig contains the configuration for the tags that are found in browser.SteamSpyAppDetails.
type TagConfig interface {
	TagDefaultValue() float64
	TagUpvotesThreshold() float64
	TagValues() map[string]float64
}

// StorefrontConfig contains the configuration for a specific Storefront.
type StorefrontConfig interface {
	StorefrontStorefront() Storefront
	StorefrontTags() TagConfig
}

type ScrapeConstants interface {
	DefaultMaxTries() int
	DefaultMinDelay() time.Duration
}

type EvaluatorConfig interface {
	EvaluatorModelName() string
	EvaluatorField() string
	EvaluatorExpression() string
	EvaluatorCompiledExpression() *vm.Program
	WeightAndInverse() (float64, bool)
	ModelType() reflect.Type
	ModelField(modelInstance any) any
	Env(modelInstance any) map[string]any
	Eval(modelInstance any) ([]float64, error)
}

// ScrapeConfig contains the configuration for the scrape.
type ScrapeConfig interface {
	ScrapeDebug() bool
	ScrapeStorefronts() []StorefrontConfig
	ScrapeGetStorefront(storefront Storefront) StorefrontConfig
	ScrapeConstants() ScrapeConstants
	ScrapeFieldEvaluatorForWeightedModelField(model any, field string) (evaluator EvaluatorConfig, err error)
	ScrapeEvalForWeightedModelField(modelInstance any, field string) (values []float64, err error)
	ScrapeWeightedModelCalc(modelInstance any) (weightedAverage float64, err error)
}

// scrapeConfig is a package-wide variable containing the ScrapeConfig, this is necessary for calculating the weighted
// score of model instances.
var scrapeConfig ScrapeConfig

// SetScrapeConfig will set the package-wide ScrapeConfig variable for the models package.
func SetScrapeConfig(config ScrapeConfig) {
	scrapeConfig = config
}

// ScrapableGameSource represents a source that can be scraped for a GameModelStorefrontScraper for a Storefront.
type ScrapableGameSource int

const (
	// Info fetches general information on the game. Such as the game's name and publisher.
	Info ScrapableGameSource = iota
	// Reviews fetches information surrounding reviews for the game. Such as total positive and negative reviews.
	Reviews
	// Community fetches information surrounding the community for a game. Such as community post upvotes and downvotes.
	Community
	// Tags fetches information surrounding the game's tags or genre.
	Tags
	// Extra is any extra information that needs to be fetched that is not in one of the above
	Extra
)

// String returns the name of the ScrapableGameSource.
func (sgs ScrapableGameSource) String() string {
	switch sgs {
	case Info:
		return "Info"
	case Reviews:
		return "Reviews"
	case Community:
		return "Community"
	case Tags:
		return "Tags"
	case Extra:
		return "Extra"
	default:
		return "<nil>"
	}
}

// Sources returns all the possible ScrapableGameSource.
func (sgs ScrapableGameSource) Sources() []ScrapableGameSource {
	return []ScrapableGameSource{Info, Reviews, Community, Tags, Extra}
}

// GameModelStorefrontScraper should implement the scrape procedures for the appropriate ScrapableGameSource(s) for a
// single Storefront. Each function should modify a reference to an internal game model. Therefore, the structure of the
// implementor should resemble (and in most cases match) the structure of the implementor of the GameModelWrapper for
// the same game model.
type GameModelStorefrontScraper[ID comparable] interface {
	// Args returns the arguments to be used for the scrape functions for the ScrapableGameSource(s) as well as the
	// arguments for AfterScrape.
	Args(id ID) []any
	// GetStorefront returns the Storefront that this scraper scrapes.
	GetStorefront() Storefront
	ScrapeInfo(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error
	ScrapeReviews(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error
	ScrapeTags(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error
	ScrapeCommunity(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error
	ScrapeExtra(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error
	// AfterScrape is called after all the above methods have completed for a Storefront. It is usually used to set the
	// Storefront and Website of the internal game instance using the given args extracted from Args.
	AfterScrape(args ...any)
}

// GameModelWrapper should be implemented by a struct containing a reference to a GameModel. With this interface you
// can access the GameModelStorefrontScraper(s) for each Storefront as well as setting the Default state of the
// GameModel instance, nilling out the GameModel instance, or retrieving the GameModel instance. ID is the type of the
// ID that is used to represent a unique GameModel instance. For example, Game.Website is the unique ID used for Game,
// so the implemented type will use string in its type parameters.
type GameModelWrapper[ID comparable, GMT GameModel[ID]] interface {
	// Default will set the fields of the internal game model instance to their default values as well as create the
	// internal game model instance if it is nil.
	Default()
	// Nil will set the internal game model instance to nil.
	Nil()
	// Get will return the GameModel instance that is referenced internally.
	Get() GMT
	// StorefrontScraper will return the GameModelStorefrontScraper for the given Storefront. This will allow you to
	// scrape the information for the internal game model instance off of this Storefront.
	StorefrontScraper(storefront Storefront) GameModelStorefrontScraper[ID]
}

// GameModel should be implemented by the gorm.Model that represents a table of games.
type GameModel[ID comparable] interface {
	// Update will update a game model instance by running the scrape procedures for its GameModelWrapper.
	Update(db *gorm.DB, config ScrapeConfig) error
	// Wrapper returns the GameModelWrapper for this GameModel.
	Wrapper() GameModelWrapper[ID, GameModel[ID]]
	// GetID returns the unique ID value of the GameModel.
	GetID() ID
}

// UnscrapableStorefront is an error that implements the GameModelStorefrontScraper interface so that it can be returned
// by implementors of the GameModelWrapper.StorefrontScraper in cases where the scrape procedures for a certain
// Storefront are not yet implemented/will never be implemented.
type UnscrapableStorefront[ID comparable] struct {
	Storefront Storefront
	Source     ScrapableGameSource
}

func (usw *UnscrapableStorefront[ID]) Args(id ID) []any          { return []any{} }
func (usw *UnscrapableStorefront[ID]) GetStorefront() Storefront { return UnknownStorefront }

func (usw *UnscrapableStorefront[ID]) ScrapeInfo(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	usw.Source = Info
	return usw
}

func (usw *UnscrapableStorefront[ID]) ScrapeReviews(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	usw.Source = Reviews
	return usw
}

func (usw *UnscrapableStorefront[ID]) ScrapeTags(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	usw.Source = Tags
	return usw
}

func (usw *UnscrapableStorefront[ID]) ScrapeCommunity(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	usw.Source = Community
	return usw
}

func (usw *UnscrapableStorefront[ID]) ScrapeExtra(config ScrapeConfig, maxTries int, minDelay time.Duration, args ...any) error {
	usw.Source = Extra
	return usw
}

func (usw *UnscrapableStorefront[ID]) AfterScrape(args ...any) {}

func (usw *UnscrapableStorefront[ID]) Error() string {
	return fmt.Sprintf(
		"scraping %s for %s is not possible/implemented",
		usw.Source.String(), usw.Storefront.String(),
	)
}

// UnscrapableError returns the UnscrapableStorefront error as a GameModelStorefrontScraper to be used as a default case
// in the implemented GameModelWrapper.StorefrontWrapper methods in case a Storefront cannot be scraped.
func UnscrapableError[ID comparable](storefront Storefront) GameModelStorefrontScraper[ID] {
	return &UnscrapableStorefront[ID]{Storefront: storefront}
}

// storefrontScraperJob is a job queued up for a worker running within the StorefrontScrapers. The job represents either
// a call to ScrapeStorefrontsForGameModel or GameModel.Wrapper (if update is set).
type storefrontScraperJob[ID comparable] struct {
	update        bool
	game          GameModel[ID]
	storefrontIDs map[Storefront]mapset.Set[ID]
	result        chan GameModel[ID]
}

// StorefrontScrapers represents a manager for multiple workers that execute calls to ScrapeStorefrontsForGameModel or
// GameModel.Wrapper (if update is set). Note that this is only valid for jobs of one type of GameModel.
type StorefrontScrapers[ID comparable] struct {
	wg                sync.WaitGroup
	workers           int
	jobs              chan *storefrontScraperJob[ID]
	guard             chan struct{}
	config            ScrapeConfig
	db                *gorm.DB
	minWorkerWaitTime time.Duration
	maxWorkerWaitTime time.Duration
	Timeout           time.Duration
	started           bool
	finished          bool
}

// NewStorefrontScrapers creates a new StorefrontScrapers with the given arguments.
// • config: a ScrapeConfig for use in ScrapeStorefrontsForGameModel.
//
// • db: a reference to a gorm.DB for use in GameModel.Update
//
// • workers: the number of workers to spin up on StorefrontScrapers.Start
//
// • maxConcurrentWorkers: the maximum number of workers that can be executing a job at the same time
//
// • maxJobs: the maximum number of jobs the buffered channel for the jobs can hold
//
// • minWorkerWaitTime: the minimum time a worker should wait after completing a job
//
// • maxWorkerWaitTime: the maximum time a worker should wait after completing a job.
//
// • timeout: the duration after which a worker should time out.
func NewStorefrontScrapers[ID comparable](
	config ScrapeConfig,
	db *gorm.DB,
	workers, maxConcurrentWorkers, maxJobs int,
	minWorkerWaitTime, maxWorkerWaitTime time.Duration,
) *StorefrontScrapers[ID] {
	return &StorefrontScrapers[ID]{
		wg:                sync.WaitGroup{},
		workers:           workers,
		jobs:              make(chan *storefrontScraperJob[ID], maxJobs),
		guard:             make(chan struct{}, maxConcurrentWorkers),
		config:            config,
		db:                db,
		minWorkerWaitTime: minWorkerWaitTime,
		maxWorkerWaitTime: maxWorkerWaitTime,
		Timeout:           time.Minute + (time.Second * 30),
	}
}

// Start the StorefrontScrapers workers.
func (ss *StorefrontScrapers[ID]) Start() {
	for i := 0; i < ss.workers; i++ {
		ss.wg.Add(1)
		go ss.worker(i + 1)
	}
	ss.started = true
}

// Jobs returns the number of unprocessed jobs left.
func (ss *StorefrontScrapers[ID]) Jobs() int {
	return len(ss.jobs)
}

// cleanupResultChannels takes a slice of result channels that can be written to, and writes nil to all of them in a
// pool of 20 workers. This is to be called on the result channels of the jobs dropped by StorefrontScrapers.Drop and
// StorefrontScrapers.DropAll to stop any consumers of these result channels from going into a deadlock.
func cleanupResultChannels[ID comparable](resultChannels []chan<- GameModel[ID]) {
	cleanupJobs := make(chan (chan<- GameModel[ID]), len(resultChannels))
	var cleanupWorkerWg sync.WaitGroup
	for w := 0; w < 20; w++ {
		cleanupWorkerWg.Add(1)
		go func() {
			defer cleanupWorkerWg.Done()
			for job := range cleanupJobs {
				job <- nil
			}
		}()
	}

	for _, result := range resultChannels {
		cleanupJobs <- result
	}
	close(cleanupJobs)
	cleanupWorkerWg.Wait()
}

// Drop will try and dequeue the given number of jobs from the StorefrontScrapers. These jobs will just be dropped
// (i.e. not scraped) and each of their result channels will be filled with a nil value so that any consumers blocking
// for these channels won't cause a deadlock. Because of this, this method will block until the resulting channels for
// each dropped job have been filled with nils. The actual number of jobs dropped will be returned.
func (ss *StorefrontScrapers[ID]) Drop(no int) int {
	dropped := 0
	if ss.started && !ss.finished {
		resultChannels := make([]chan<- GameModel[ID], 0)
	done:
		for i := 0; i < no; i++ {
			select {
			case job := <-ss.jobs:
				resultChannels = append(resultChannels, job.result)
			default:
				break done
			}
		}

		dropped = len(resultChannels)
		if dropped > 0 {
			cleanupResultChannels[ID](resultChannels)
		}
	}
	return dropped
}

// DropAll works similarly to Drop, but it will instead keep de-queueing jobs from the job channel until there are no
// more left. Because of this, this method will block until the resulting channels for each dropped job have been filled
// with nils. It will return the actual number of jobs that have been dropped.
func (ss *StorefrontScrapers[ID]) DropAll() int {
	dropped := 0
	if ss.started && !ss.finished {
		resultChannels := make([]chan<- GameModel[ID], 0)
	done:
		for {
			select {
			case job := <-ss.jobs:
				resultChannels = append(resultChannels, job.result)
			default:
				break done
			}
		}

		dropped = len(resultChannels)
		if dropped > 0 {
			cleanupResultChannels[ID](resultChannels)
		}
	}
	return dropped
}

// Add adds a job to be executed by the started workers. This returns a channel that can be read from to block until the
// game has been scraped, and will also return a boolean value indicating whether the job was queued.
func (ss *StorefrontScrapers[ID]) Add(update bool, gameModel GameModel[ID], storefrontIDs map[Storefront]mapset.Set[ID]) (<-chan GameModel[ID], bool) {
	scrapeGameDone := make(chan GameModel[ID])
	ok := false
	if ss.started && !ss.finished {
		select {
		case ss.jobs <- &storefrontScraperJob[ID]{
			update:        update,
			game:          gameModel,
			storefrontIDs: storefrontIDs,
			result:        scrapeGameDone,
		}:
			ok = true
		default:
			log.ERROR.Printf(
				"StorefrontScrapers: could not queue scrape for %v as it will exceed the job capacity of %d",
				gameModel.GetID(), cap(ss.jobs),
			)
		}
	}
	return scrapeGameDone, ok
}

// Wait waits for the workers to finish. It should only be called after all jobs have been added using the Add method.
// This is because Wait will close the job and guard channels.
func (ss *StorefrontScrapers[ID]) Wait() {
	if ss.started && !ss.finished {
		close(ss.jobs)
		ss.wg.Wait()
		close(ss.guard)
		ss.finished = true
	}
}

// Stop tries to stop the StorefrontScrapers as fast as possible by first dropping all the remaining jobs in the queue,
// and then calling Wait to wait for all the currently running workers to finish the jobs they are currently processing.
func (ss *StorefrontScrapers[ID]) Stop() {
	if ss.started && !ss.finished {
		ss.DropAll()
		ss.Wait()
	}
}

// workerExecuteJob will execute a storefrontScraperJob for a StorefrontScrapers.worker with the Timeout provided in the
// StorefrontScrapers.
func (ss *StorefrontScrapers[ID]) workerExecuteJob(workerNo int, job *storefrontScraperJob[ID]) bool {
	done := make(chan struct{}, 1)
	go func() {
		if job.update {
			if err := job.game.Update(ss.db, ss.config); err != nil {
				log.WARNING.Printf(
					"StorefrontScraper worker no. %d could not save game model instance %s: %s",
					workerNo, job.game.GetID(), err.Error(),
				)
			}
		} else {
			job.game = ScrapeStorefrontsForGameModel[ID](job.game, job.storefrontIDs, ss.config)
		}
		done <- struct{}{}
	}()

	select {
	case <-time.After(ss.Timeout):
		return false
	case <-done:
		return true
	}
}

// worker is a worker procedure that should be run in a separate goroutine.
func (ss *StorefrontScrapers[ID]) worker(no int) {
	defer ss.wg.Done()
	time.Sleep(ss.maxWorkerWaitTime * time.Duration(no))
	log.INFO.Printf("ScrapeStorefrontsForGameWorker no. %d has started...", no)
	jobTotal := 0
	for job := range ss.jobs {
		ss.guard <- struct{}{}
		log.INFO.Printf(
			"StorefrontScraper no. %d has acquired the guard, and is now executing a job marked as "+
				"update = %t",
			no, job.update,
		)

		// Find out how long to sleep for...
		min := int64(ss.minWorkerWaitTime)
		max := int64(ss.maxWorkerWaitTime)
		random := rand.Int63n(max-min+1) + min
		sleepDuration := time.Duration(random)

		// Execute the job, sometimes this can time out...
		if ok := ss.workerExecuteJob(no, job); ok {
			log.INFO.Printf(
				"StorefrontScraper no. %d has completed a job and is now sleeping for %s",
				no, sleepDuration.String(),
			)
			job.result <- job.game
			jobTotal++
		} else {
			log.WARNING.Printf(
				"StorefrontScraper worker no. %d timed out (after %s) when executing job. We are still going to "+
					"sleep for %s",
				no, ss.Timeout.String(), sleepDuration.String(),
			)
			job.result <- nil
		}

		time.Sleep(sleepDuration)
		<-ss.guard
	}
	log.INFO.Printf("StorefrontScraper no. %d has finished. Executed %d jobs", no, jobTotal)
}

// ScrapeStorefrontForGameModel scrapes a single Storefront for a GameModel using their GameModelStorefrontScraper
// instance.
func ScrapeStorefrontForGameModel[ID comparable](id ID, gameModelScraper GameModelStorefrontScraper[ID], config ScrapeConfig) {
	// Do some scraping to find more details for the game
	args := gameModelScraper.Args(id)
	scrapeMaxTries := config.ScrapeConstants().DefaultMaxTries()
	scrapeMinDelay := config.ScrapeConstants().DefaultMinDelay()
	for _, source := range Info.Sources() {
		var err error
		switch source {
		case Info:
			err = gameModelScraper.ScrapeInfo(config, scrapeMaxTries, scrapeMinDelay, args...)
		case Reviews:
			err = gameModelScraper.ScrapeReviews(config, scrapeMaxTries, scrapeMinDelay, args...)
		case Community:
			err = gameModelScraper.ScrapeCommunity(config, scrapeMaxTries, scrapeMinDelay, args...)
		case Tags:
			err = gameModelScraper.ScrapeTags(config, scrapeMaxTries, scrapeMinDelay, args...)
		case Extra:
			err = gameModelScraper.ScrapeExtra(config, scrapeMaxTries, scrapeMinDelay, args...)
		default:
			err = fmt.Errorf("cannot scrape ScrapableGameSource %d for game", source)
		}

		if err != nil {
			log.WARNING.Printf(
				"Could not scrape %s for %s for game on ID %v: %s",
				source.String(), gameModelScraper.GetStorefront().String(), id, err.Error(),
			)
		}
	}
	// Finally, execute the AfterScrape method which usually sets the Storefront and Website fields
	gameModelScraper.AfterScrape(args...)
}

// ScrapeStorefrontsForGameModel will run the scrape procedure for all the Storefront that exist in the given map.
// Each Storefront can have a set of IDs/URLs. However, only one of these will ever be scraped and this is chosen at
// random.
func ScrapeStorefrontsForGameModel[ID comparable](gameModel GameModel[ID], storefrontIDs map[Storefront]mapset.Set[ID], config ScrapeConfig) GameModel[ID] {
	gameModelWrapper := gameModel.Wrapper()
	if len(storefrontIDs) > 0 {
		gameModelWrapper.Default()
		for _, storefront := range UnknownStorefront.Storefronts() {
			if _, ok := storefrontIDs[storefront]; ok {
				storefrontWrapper := gameModelWrapper.StorefrontScraper(storefront)
				if _, ok = storefrontWrapper.(*UnscrapableStorefront[ID]); !ok {
					// Pop a random URL from the set of URLs for this storefront
					id, _ := storefrontIDs[storefront].Pop()
					// Do some scraping to find more details for the game
					ScrapeStorefrontForGameModel(id, storefrontWrapper, config)
				} else {
					log.WARNING.Printf(
						"%s scrape for %s is not implemented",
						storefront.String(), reflect.TypeOf(storefrontWrapper).String(),
					)
				}
			}
		}
	} else {
		gameModelWrapper.Nil()
	}
	return gameModelWrapper.Get()
}
