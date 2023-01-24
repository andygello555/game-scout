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
	"time"
)

const (
	maxTrendWorkers       = 10
	maxTrendingDevelopers = 10
	maxTopSteamApps       = 10
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

	topDevelopers := newTrendFinderResultHeap(false)
	for i := 0; i < len(enabledDevelopers); i++ {
		result := <-results
		if result.err != nil {
			log.ERROR.Printf(
				"Error occurred when trying to find trend for Developer %v: %v, skipping...",
				result.Developer, result.err,
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
			"%d: Adding Developer %v with trend %.10f to email context...",
			i+1, topDeveloper.Developer, topDeveloper.Trend.GetCoeffs()[1],
		)
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
	).Limit(maxTopSteamApps).Find(&steamApps).Error; err != nil {
		return myErrors.TemporaryWrapf(false, err, "could not find the top %d SteamApps", maxTopSteamApps)
	}
	measureContext.TopSteamApps = steamApps

	if !debug {
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
	log.INFO.Println(resp.Email.Profiling.String())
	return
}
