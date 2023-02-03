package main

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/errors"
	"github.com/andygello555/game-scout/twitter"
	"github.com/deckarep/golang-set/v2"
	"github.com/google/uuid"
	errors2 "github.com/pkg/errors"
	"strings"
	"time"
)

// Phase represents a phase in the Scout procedure.
type Phase int

const (
	Discovery Phase = iota
	Update
	Snapshot
	Disable
	Enable
	Delete
	Measure
	Done
)

// String returns the name of the Phase.
func (p Phase) String() string {
	switch p {
	case Discovery:
		return "Discovery"
	case Update:
		return "Update"
	case Snapshot:
		return "Snapshot"
	case Disable:
		return "Disable"
	case Enable:
		return "Enable"
	case Delete:
		return "Delete"
	case Measure:
		return "Measure"
	case Done:
		return "Done"
	default:
		return "<nil>"
	}
}

func (p Phase) Run(state *ScoutState) (err error) {
	var phaseStart time.Time
	phaseBefore := func() {
		phaseStart = time.Now().UTC()
		state.GetCachedField(StateType).SetOrAdd("PhaseStart", phaseStart)
		phase, _ := state.GetCachedField(StateType).Get("Phase")
		log.INFO.Printf("Starting %s phase", phase.(Phase).String())
	}

	phaseAfter := func() {
		phase, _ := state.GetCachedField(StateType).Get("Phase")
		log.INFO.Printf("Finished %s phase in %s", phase.(Phase).String(), time.Now().UTC().Sub(phaseStart).String())
		nextPhase := phase.(Phase).Next()
		state.GetCachedField(StateType).SetOrAdd("Phase", nextPhase)
		if err = state.Save(); err != nil {
			log.ERROR.Printf("Could not save ScoutState: %v", err)
		}
	}

	phase, _ := state.GetCachedField(StateType).Get("Phase")
	if phase.(Phase) != p {
		return fmt.Errorf(
			"state's saved phase (%s) does not much the phase being run (%s)",
			phase.(Phase).String(), p.String(),
		)
	}

	switch phase.(Phase) {
	case Discovery:
		discoveryTweetsAny, _ := state.GetCachedField(StateType).Get("DiscoveryTweets")
		discoveryTweets := discoveryTweetsAny.(int)

		// If the number of discovery tweets we requested is more than maxDiscoveryTweetsDailyPercent of the total tweets
		// available for a day then we will error out.
		if float64(discoveryTweets) > globalConfig.Scrape.Constants.maxTotalDiscoveryTweets() {
			return errors.TemporaryErrorf(
				false,
				"cannot request more than %d tweets per day for discovery purposes",
				int(twitter.TweetsPerDay),
			)
		}

		phaseBefore()

		// 1st phase: Discovery. Search the recent tweets on Twitter belonging to the hashtags given in config.json.
		var subGameIDs mapset.Set[uuid.UUID]
		if subGameIDs, err = DiscoveryPhase(state); err != nil {
			return err
		}
		state.GetIterableCachedField(GameIDsType).Merge(&GameIDs{Set: subGameIDs})

		phaseAfter()
	case Update:
		// 2nd phase: Update. For any developers, and games that exist in the DB but haven't been scraped we will fetch the
		// details from the DB and initiate scrapes for them
		log.INFO.Printf("We have scraped:")
		log.INFO.Printf("\t%d developers", state.GetIterableCachedField(DeveloperSnapshotsType).Len())
		log.INFO.Printf("\t%d games", state.GetIterableCachedField(DeveloperSnapshotsType).Len())

		phaseBefore()

		// Extract the IDs of the developers that were just scraped in the discovery phase.
		scrapedDevelopers := make([]string, state.GetIterableCachedField(DeveloperSnapshotsType).Len())
		iter := state.GetIterableCachedField(DeveloperSnapshotsType).Iter()
		for iter.Continue() {
			scrapedDevelopers[iter.I()] = iter.Key().(string)
			iter.Next()
		}

		// If there are already developers that have been updated in a previously run UpdatePhase, then we will add
		// these to the scrapedDevelopers also
		stateState := state.GetCachedField(StateType).(*State)
		if len(stateState.UpdatedDevelopers) > 0 {
			scrapedDevelopers = append(scrapedDevelopers, stateState.UpdatedDevelopers...)
		}

		// Run the UpdatePhase.
		if err = UpdatePhase(scrapedDevelopers, state); err != nil {
			return err
		}

		phaseAfter()
	case Snapshot:
		// 3rd phase: Snapshot. For all the partial DeveloperSnapshots that exist in the developerSnapshots map we will
		// aggregate the information for them.
		phaseBefore()

		if err = SnapshotPhase(state); err != nil {
			return errors2.Wrap(err, "snapshot phase has failed")
		}

		phaseAfter()
	case Disable:
		// 4th phase: Disable. Disable the developers who aren't doing so well. The number of developers to disable depends
		// on the number of tweets we get to use in the update phase the next time around.
		phaseBefore()

		if err = DisablePhase(state); err != nil {
			return errors2.Wrap(err, "disable phase has failed")
		}

		phaseAfter()
	case Enable:
		// 5th phase: Enable: Enable a certain amount of developers who were disabled before today based off of how well
		// their snapshots have been trending.
		phaseBefore()

		if err = EnablePhase(state); err != nil {
			return errors2.Wrap(err, "enable phase has failed")
		}

		phaseAfter()
	case Delete:
		// 6th phase: Delete. Delete a certain percentage of the disabled developer that are performing the worst. This
		// is decided by calculating their trend so far.
		phaseBefore()

		if err = DeletePhase(state); err != nil {
			return errors2.Wrap(err, "delete phase has failed")
		}

		phaseAfter()
	case Measure:
		// 7th phase: Measure. For all the users that have a good number of snapshots we'll see if any are shining above
		// the rest
		phaseBefore()

		if err = MeasurePhase(state); err != nil {
			return errors2.Wrap(err, "measure phase has failed")
		}

		phaseAfter()
	default:
		// This will be Done which we don't need to do anything for.
	}
	return
}

// PhaseFromString returns the Phase that has the given name. If there is no Phase of that name, then Discovery will be
// returned instead. Case-insensitive.
func PhaseFromString(s string) Phase {
	switch strings.ToLower(s) {
	case "discovery":
		return Discovery
	case "update":
		return Update
	case "snapshot":
		return Snapshot
	case "disable":
		return Disable
	case "enable":
		return Enable
	case "delete":
		return Delete
	case "measure":
		return Measure
	case "done":
		return Done
	default:
		return Discovery
	}
}

// Next returns the Phase after this one. If the referred to Phase is the last phase (Done) then the last Phase will be
// returned.
func (p Phase) Next() Phase {
	if p == Done {
		return p
	}
	return p + 1
}
