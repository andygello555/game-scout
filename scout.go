package main

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	myErrors "github.com/andygello555/agem"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/game-scout/email"
	"github.com/pkg/errors"
	"github.com/schollz/progressbar/v3"
	"math"
	"runtime/pprof"
	"strings"
	"time"
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
		err = errors.Wrapf(err, "could not load/create ScoutState when starting Scout procedure")
		return
	}

	defer func(state *ScoutState) {
		// Check if a panic has occurred. If so, we will merge it into the currently set error return parameter.
		p := recover()
		if p != nil {
			if pErr, ok := p.(error); ok {
				err = myErrors.MergeErrors(err, pErr)
			} else {
				err = myErrors.MergeErrors(err, fmt.Errorf("%v", p))
			}
		}

		// If an error exists, we will send the Error email Template.
		if err != nil {
			err = myErrors.MergeErrors(err, state.Save())
			log.ERROR.Printf("Error occurred in Scout procedure, sending error email: %v", err)
			// Send the Error email Template
			context := &ErrorContext{
				Time:        time.Now().UTC(),
				Error:       err,
				State:       state,
				StackTraces: new(strings.Builder),
			}
			traceErr := pprof.Lookup("goroutine").WriteTo(context.StackTraces, 1)
			template := context.Execute()
			if err = myErrors.MergeErrors(err, traceErr, template.Error, template.SendSync().Error); err != nil {
				log.ERROR.Printf("Errors after sending error email: %v", err)
			}
		}
	}(state)

	result := make(chan error, 1)
	go func(state *ScoutState) {
		if state.Loaded {
			log.WARNING.Printf("Previous ScoutState \"%s\" was loaded from disk so we are ignoring batchSize and discoveryTweets parameters:", state.BaseDir())
			log.WARNING.Println(state.String())

			// Set the discoveryTweets and batchSize parameters according to the saved values in ScoutState
			batchSizeAny, _ := state.GetCachedField(StateType).Get("BatchSize")
			batchSize = batchSizeAny.(int)
			discoveryTweetsAny, _ := state.GetCachedField(StateType).Get("DiscoveryTweets")
			discoveryTweets = discoveryTweetsAny.(int)
		} else {
			// If no state has been loaded, then we'll assume that the previous run of Scout was successful and set up
			// ScoutState as usual
			state.GetCachedField(StateType).SetOrAdd("Start", time.Now().UTC())
			state.GetCachedField(StateType).SetOrAdd("Result", "Started", time.Now().UTC())
			state.GetCachedField(StateType).SetOrAdd("Phase", Discovery)
			state.GetCachedField(StateType).SetOrAdd("BatchSize", batchSize)
			state.GetCachedField(StateType).SetOrAdd("DiscoveryTweets", discoveryTweets)
		}
		// We always set the Debug flag in the State to be the same as the debug flag in the ScrapeConfig.
		state.GetCachedField(StateType).SetOrAdd("Debug", globalConfig.Scrape.Debug)

		// Send the Started email Template
		var startedTemplate *email.Template
		startedContext := &StartedContext{State: state}
		if startedTemplate = startedContext.Execute(); startedTemplate.Error != nil {
			log.ERROR.Printf("Could not fill Started Template with StartedContext: %v", startedTemplate.Error)
		}
		startedTemplate.SendAsyncAndConsume(func(resp email.Response) {
			if resp.Error != nil {
				log.ERROR.Printf("Error occurred whilst sending Started Template: %v", resp.Error)
			} else {
				log.INFO.Printf("Successfully sent Started email in %s", resp.Email.Profiling.Total().String())
			}
		})

		getPhase := func() Phase {
			phase, _ := state.GetCachedField(StateType).Get("Phase")
			return phase.(Phase)
		}

		// Loop over all the Phases and Run each one until the done phase
		for phase := getPhase(); phase != Done; phase = getPhase() {
			if err = phase.Run(state); err != nil {
				result <- err
				return
			}
		}

		// Save the ScoutResult
		scoutResultAny, _ := state.GetCachedField(StateType).Get("Result")
		if err = db.DB.Create(scoutResultAny).Error; err != nil {
			result <- errors.Wrap(err, "could not create ScoutResult")
			return
		}
		log.INFO.Println("Saved ScoutResult")

		// Set the Finished time in ScoutState, then retrieve the Start and Finished times to calculate the duration.
		state.GetCachedField(StateType).SetOrAdd("Finished", time.Now().UTC())
		start, _ := state.GetCachedField(StateType).Get("Start")
		finished, _ := state.GetCachedField(StateType).Get("Finished")

		// Send the Finished email Template
		var finishedTemplate *email.Template
		finishedContext := &email.FinishedContext{
			BatchSize:       batchSize,
			DiscoveryTweets: discoveryTweets,
			Started:         start.(time.Time),
			Finished:        finished.(time.Time),
			Result:          scoutResultAny.(*models.ScoutResult),
		}
		if finishedTemplate = finishedContext.Execute(); finishedTemplate.Error != nil {
			log.ERROR.Printf("Could not fill Finished Template with FinishedContext: %v", finishedTemplate.Error)
		}
		if resp := finishedTemplate.SendSync(); resp.Error != nil {
			log.ERROR.Printf("Error occurred whilst sending Finished Template: %v", resp.Error)
		} else {
			log.INFO.Printf("Successfully sent Finished email in %s", resp.Email.Profiling.Total().String())
		}

		log.INFO.Printf("Finished Scout in %s", finishedContext.Finished.Sub(finishedContext.Started))

		// Finally, delete the state
		state.Delete()
		result <- nil
	}(state)

	select {
	case <-time.After(globalConfig.Scrape.Constants.ScoutTimeout.Duration):
		err = fmt.Errorf(
			"scout procedure has been running for longer than %s, shutting it down",
			globalConfig.Scrape.Constants.ScoutTimeout.String(),
		)
	case err = <-result:
		break
	}
	return
}
