package main

import (
	"container/heap"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/db"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/game-scout/email"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"math"
	"os"
	"time"
)

const (
	maxTrendWorkers       = 10
	maxTrendingDevelopers = 10
	maxTopSteamApps       = 10
)

type trendFinderResult struct {
	err error
	*email.TrendingDev
}

type trendFinderResultHeap []*trendFinderResult

func (h trendFinderResultHeap) Len() int { return len(h) }
func (h trendFinderResultHeap) Less(i, j int) bool {
	return h[i].Trend.GetCoeffs()[1] > h[j].Trend.GetCoeffs()[1]
}
func (h trendFinderResultHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *trendFinderResultHeap) Push(x any) { *h = append(*h, x.(*trendFinderResult)) }

func (h *trendFinderResultHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func trendFinder(jobs <-chan *models.Developer, results chan<- *trendFinderResult) {
	for job := range jobs {
		result := &trendFinderResult{
			TrendingDev: &email.TrendingDev{
				Developer: job,
			},
		}

		if result.Games, result.err = job.Games(db.DB); result.err != nil {
			result.err = errors.Wrapf(result.err, "trendFinder cannot find games for %s (%s)", result.Developer.Username, result.Developer.ID)
			results <- result
			continue
		}
		log.INFO.Printf("Developer %s (%s) has %d games", job.Username, job.ID, len(result.Games))

		if result.Snapshots, result.err = job.DeveloperSnapshots(db.DB); result.err != nil {
			result.err = errors.Wrapf(result.err, "trendFinder cannot find snapshots for %s (%s)", result.Developer.Username, result.Developer.ID)
			results <- result
			continue
		}
		log.INFO.Printf("Developer %s (%s) has %d snapshots", job.Username, job.ID, len(result.Snapshots))

		if result.Trend, result.err = job.Trend(db.DB); result.err != nil {
			result.err = errors.Wrapf(result.err, "trendFinder cannot find trend for %s (%s)", result.Developer.Username, result.Developer.ID)
		} else {
			log.INFO.Printf("Found trend for developer %s (%s) = %.2f", job.Username, job.ID, result.Trend.GetCoeffs()[1])
		}
		results <- result
	}
}

func MeasurePhase(state *ScoutState) (err error) {
	measureContext := email.MeasureContext{
		TrendingDevs:           make([]*email.TrendingDev, 0),
		DevelopersBeingDeleted: *state.GetIterableCachedField(DeletedDevelopersType).(*DeletedDevelopers),
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

	if err = db.DB.Model(&models.Developer{}).Where("not disabled").Count(&measureContext.EnabledDevelopers).Error; err != nil {
		log.ERROR.Printf("Could not find the number of enabled developers: %v", err)
	}
	log.INFO.Printf(
		"There are %d enabled developers, we are going to find the trend of %d developers",
		measureContext.EnabledDevelopers, len(enabledDevelopers),
	)

	jobs := make(chan *models.Developer, len(enabledDevelopers))
	results := make(chan *trendFinderResult, len(enabledDevelopers))
	for w := 0; w < maxTrendWorkers; w++ {
		go trendFinder(jobs, results)
	}

	for _, enabledDeveloper := range enabledDevelopers {
		jobs <- enabledDeveloper
	}
	close(jobs)

	topDevelopers := make(trendFinderResultHeap, 0)
	for i := 0; i < len(enabledDevelopers); i++ {
		result := <-results
		if result.err != nil {
			log.ERROR.Printf(
				"Error occurred when trying to find trend for %s (%s): %v, skipping...",
				result.Developer.Username, result.Developer.ID, result.err,
			)
		} else {
			heap.Push(&topDevelopers, result)
		}
	}
	close(results)

	// Add the top maxTrendingDevelopers (or however many we found)
	topDevelopersNo := int(math.Min(float64(topDevelopers.Len()), maxTrendingDevelopers))
	log.INFO.Printf("Adding top %d developers to the MeasureContext", topDevelopersNo)
	for i := 0; i < topDevelopersNo; i++ {
		topDeveloper := heap.Pop(&topDevelopers).(*trendFinderResult)
		log.INFO.Printf(
			"%d: Adding Developer \"%s\" (%s) with trend %.10f to email context...",
			i+1, topDeveloper.Developer.Username, topDeveloper.Developer.ID, topDeveloper.Trend.GetCoeffs()[1],
		)
		if debug, _ := state.GetCachedField(StateType).Get("Debug"); !debug.(bool) {
			// Update the number of times this developer has been highlighted
			if err = db.DB.Model(topDeveloper.Developer).Update("times_highlighted", gorm.Expr("times_highlighted + 1")).Error; err != nil {
				log.ERROR.Printf("Could not increment TimesHighlighted for %s (%s): %v", topDeveloper.Developer.Username, topDeveloper.Developer.ID, err)
			}
		} else {
			log.WARNING.Printf(
				"ScoutState.Debug is set, so we are skipping incrementing times_highlighted on %s (%s)",
				topDeveloper.Developer.Username, topDeveloper.Developer.ID,
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
	).Limit(maxTopSteamApps).Find(&steamApps).Error; err != nil {
		return myErrors.TemporaryWrapf(false, err, "could not find the top %d SteamApps", maxTopSteamApps)
	}
	measureContext.TopSteamApps = steamApps

	if debug, _ := state.GetCachedField(StateType).Get("Debug"); !debug.(bool) {
		for _, steamApp := range steamApps {
			if err = db.DB.Model(steamApp).Update("times_highlighted", gorm.Expr("times_highlighted + 1")).Error; err != nil {
				log.ERROR.Printf("Could not increment TimesHighlighted for %s (%d): %v", steamApp.Name, steamApp.ID, err)
			}
		}
	} else {
		log.WARNING.Printf(
			"ScoutState.Debug is set, so we are skipping incrementing times_highlighted on %d SteamApps",
			len(steamApps),
		)
	}

	// Finally, we construct the email and send it
	var pdf *email.Template
	if pdf = measureContext.HTML().PDF(); pdf.Error != nil {
		return myErrors.TemporaryWrap(false, pdf.Error, "could not construct Measure PDF")
	}

	// TODO: Remove this and replace it with email sending
	if err = os.WriteFile(
		fmt.Sprintf(
			"measure_email_for_%d_developers_%s.pdf",
			len(enabledDevelopers),
			time.Now().UTC().Format("2006-01-02"),
		),
		pdf.Buffer.Bytes(),
		filePerms,
	); err != nil {
		return err
	}
	return
}
