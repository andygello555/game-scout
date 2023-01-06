package main

import (
	"github.com/RichardKnop/machinery/v1/log"
	myErrors "github.com/andygello555/game-scout/errors"
	myTwitter "github.com/andygello555/game-scout/twitter"
	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
	"math"
	"time"
)

const (
	// transformTweetWorkers is the number of transformTweetWorker that will be spun up in the DiscoveryBatch.
	transformTweetWorkers = 5
	// updateDeveloperWorkers is the number of updateDeveloperWorker that will be spun up in the update phase.
	updateDeveloperWorkers = 5
	// maxUpdateTweets is the maximum number of tweets fetched in the update phase.
	maxUpdateTweets = 12
	// secondsBetweenDiscoveryBatches is the number of seconds to sleep between DiscoveryBatch batches.
	secondsBetweenDiscoveryBatches = time.Second * 3
	// secondsBetweenUpdateBatches is the number of seconds to sleep between queue batches of updateDeveloperJob.
	secondsBetweenUpdateBatches = time.Second * 30
	// maxTotalDiscoveryTweetsDailyPercent is the maximum percentage that the discoveryTweets number can be out of
	// myTwitter.TweetsPerDay.
	maxTotalDiscoveryTweetsDailyPercent = 0.55
	// maxTotalDiscoveryTweets is the maximum number of discoveryTweets that can be given to Scout.
	maxTotalDiscoveryTweets = float64(myTwitter.TweetsPerDay) * maxTotalDiscoveryTweetsDailyPercent
	// maxTotalUpdateTweets is the maximum number of tweets that can be scraped by the Update phase.
	maxTotalUpdateTweets = float64(myTwitter.TweetsPerDay) * (1.0 - maxTotalDiscoveryTweetsDailyPercent)
	// maxEnabledDevelopers is the number of developers to keep in the Disable phase.
	maxEnabledDevelopers = maxTotalUpdateTweets / maxUpdateTweets
	// discoveryGameScrapeWorkers is the number of scrapeStorefrontsForGameWorker to start in the discovery phase.
	discoveryGameScrapeWorkers = 10
	// discoveryMaxConcurrentGameScrapeWorkers is number of scrapeStorefrontsForGameWorker that can be processing a job
	// at the same time in the discovery phase.
	discoveryMaxConcurrentGameScrapeWorkers = 9
	// updateGameScrapeWorkers is the number of scrapeStorefrontsForGameWorker to start in the UpdatePhase.
	updateGameScrapeWorkers = 10
	// updateMaxConcurrentGameScrapeWorkers is number of scrapeStorefrontsForGameWorker that can be processing a job
	// at the same time in the UpdatePhase.
	updateMaxConcurrentGameScrapeWorkers = 9
)

// sleepBar will sleep for the given time.Duration (as seconds) and display a progress bar for the sleep.
func sleepBar(sleepDuration time.Duration) {
	bar := progressbar.Default(int64(math.Ceil(sleepDuration.Seconds())))
	for s := 0; s < int(math.Ceil(sleepDuration.Seconds())); s++ {
		_ = bar.Add(1)
		time.Sleep(time.Second)
	}
}

// Scout will run the 5 phase scraping procedure for the game scout system.
//
// • 1st phase: Discovery. Search the recent tweets on Twitter belonging to the hashtags given in config.json.
//
// • 2nd phase: Update. For any developers, and games that exist in the DB that haven't been scraped in the Discovery
// phase, we will fetch their details from the DB and initiate scrapes for them to update their models.Game, create a
// models.DeveloperSnapshot, and update the models.Developer instance itself.
//
// • 3rd phase: Snapshot. For all the partial models.DeveloperSnapshot that have been created in the previous two phases
// we will aggregate the information for them into one models.DeveloperSnapshot for each models.Developer.
//
// • 4th phase: Disable. Disable all the models.Developer who aren't doing so well. The number of developers to disable
// depends on the number of tweets we get to use in the update phase the next time around:
// totalEnabledDevelopers - maxEnabledDevelopers.
//
// • 5th phase: Measure. For all the models.Developer that have a good number of models.DeveloperSnapshot we'll see if
// any are shining above the rest.
func Scout(batchSize int, discoveryTweets int) (err error) {
	var state *ScoutState
	if state, err = StateLoadOrCreate(false); err != nil {
		return errors.Wrapf(err, "could not load/create ScoutState when starting Scout procedure")
	}

	defer func() {
		// If an error occurs we'll always attempt to save the state
		if err != nil {
			err = myErrors.MergeErrors(err, state.Save())
		}
	}()

	if state.Loaded {
		log.WARNING.Printf("Previous ScoutState \"%s\" was loaded from disk so we are ignoring batchSize and discoveryTweets parameters:", state.BaseDir())
		log.WARNING.Println(state.String())
	} else {
		// If no state has been loaded, then we'll assume that the previous run of Scout was successful and set up
		// ScoutState as usual
		state.GetCachedField(StateType).SetOrAdd("Phase", Discovery)
		state.GetCachedField(StateType).SetOrAdd("BatchSize", batchSize)
		state.GetCachedField(StateType).SetOrAdd("DiscoveryTweets", discoveryTweets)
	}
	// We always set the Debug flag in the State to be the same as the debug flag in the ScrapeConfig.
	state.GetCachedField(StateType).SetOrAdd("Debug", globalConfig.Scrape.Debug)

	getPhase := func() Phase {
		phase, _ := state.GetCachedField(StateType).Get("Phase")
		return phase.(Phase)
	}

	state.GetCachedField(StateType).SetOrAdd("Start", time.Now().UTC())
	// Loop over all the Phases and Run each one until the done phase
	for phase := getPhase(); phase != Done; phase = getPhase() {
		if err = phase.Run(state); err != nil {
			return err
		}
	}

	state.GetCachedField(StateType).SetOrAdd("Finished", time.Now().UTC())
	start, _ := state.GetCachedField(StateType).Get("Start")
	finished, _ := state.GetCachedField(StateType).Get("Finished")
	log.INFO.Printf("Finished Scout in %s", finished.(time.Time).Sub(start.(time.Time)).String())

	state.Delete()
	return
}
