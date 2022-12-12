package models

import (
	"bytes"
	"fmt"
	"github.com/pkg/errors"
	"github.com/sajari/regression"
	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"
	"gorm.io/gorm"
	"reflect"
	"time"
)

// trendModel is implemented by Model instances that need to have their trends measured by one or more metrics that
// exist as a field in the model.
type trendModel interface {
	GetObservedName() string
	GetVariableNames() []string
	Train(trend *Trend) error
}

// Trend references the structure used to calculate the regression of the observed variable for a trendModel. This will
// only work for a regression that has the timestamp of when each trendModel instance was created as a variable that
// affects the regression's predictions.
type Trend struct {
	model      trendModel
	r          *regression.Regression
	dataPoints regression.DataPoints
	db         *gorm.DB
}

// NewTrend creates a new trend and sets the observed value and variables that are used for the regression.
func NewTrend(db *gorm.DB, model trendModel) *Trend {
	r := regression.Regression{}
	r.SetObserved(model.GetObservedName())
	for i, name := range model.GetVariableNames() {
		r.SetVar(i, name)
	}

	return &Trend{
		model: model,
		r:     &r,
		db:    db,
	}
}

// AddDataPoint will add a data point for the given date (variable) and score (observed value). It can also be given
// extra variables in case you wanted to do polynomial regression.
func (trend *Trend) AddDataPoint(date time.Time, score float64, extraVars ...float64) {
	if trend.dataPoints == nil {
		trend.dataPoints = make(regression.DataPoints, 0)
	}

	year, month, day := date.Date()
	vars := []float64{float64(time.Date(year, month, day, 0, 0, 0, 0, time.UTC).UnixNano())}
	if len(extraVars) > 0 {
		vars = append(vars, extraVars...)
	}

	dp := regression.DataPoint(score, vars)
	trend.dataPoints = append(trend.dataPoints, dp)
	trend.r.Train(dp)
}

// Train will add the training data of the model instance to the regression model. This will not run the training
// itself.
func (trend *Trend) Train() (err error) {
	if err = trend.model.Train(trend); err != nil {
		err = errors.Wrapf(err, "could not Train instance of %s", reflect.TypeOf(trend.model).Elem().String())
	}
	return
}

// Trend will run the training for the regression model and return the coefficients for the resulting regression line's
// formula.
func (trend *Trend) Trend() (coefficients []float64, err error) {
	if err = trend.r.Run(); err != nil {
		err = errors.Wrapf(err, "could not find Trend of %s", reflect.TypeOf(trend.model).Elem().String())
	}
	coefficients = trend.r.GetCoeffs()
	return
}

// Chart generates a chart for the Trend with the given size. The returned bytes.Buffer can be saved as a PNG.
func (trend *Trend) Chart(width, height int) (buffer *bytes.Buffer, err error) {
	times := make([]time.Time, len(trend.dataPoints))

	// Time series for the observed variable
	observedTimeSeries := chart.TimeSeries{
		Name: fmt.Sprintf("%s over time", trend.model.GetObservedName()),
		Style: chart.Style{
			Show:        true,
			StrokeColor: chart.GetDefaultColor(0).WithAlpha(64),
			StrokeWidth: 3,
		},
		YValues: make([]float64, len(trend.dataPoints)),
	}

	// Regression time series. Uses the predicted values at each time to construct the regression line.
	predictedTimeSeries := chart.TimeSeries{
		Name: fmt.Sprintf("Regression for %s over time", trend.model.GetObservedName()),
		Style: chart.Style{
			Show:            true,
			StrokeColor:     drawing.ColorRed,
			StrokeDashArray: []float64{5.0, 5.0},
			StrokeWidth:     3,
		},
		YValues: make([]float64, len(trend.dataPoints)),
	}

	annotationSeries := chart.AnnotationSeries{
		Name: "Gradient annotation",
		Style: chart.Style{
			Show:        true,
			StrokeColor: chart.GetDefaultColor(2).WithAlpha(64),
			FillColor:   chart.GetDefaultColor(2).WithAlpha(64),
		},
	}

	// Insert all the data into the chart
	for i, dp := range trend.dataPoints {
		times[i] = time.Unix(0, int64(dp.Variables[0]))
		observedTimeSeries.YValues[i] = dp.Observed

		var trendY float64
		if trendY, err = trend.r.Predict(dp.Variables); err != nil {
			err = errors.Wrapf(
				err,
				"could not predict %ss %s for %s",
				reflect.TypeOf(trend.model).Elem().String(),
				trend.model.GetVariableNames()[0],
				times[i].String(),
			)
		}
		predictedTimeSeries.YValues[i] = trendY

		if i == len(trend.dataPoints)/2 {
			annotation := chart.Value2{
				XValue: float64(times[i].UnixNano()),
				YValue: predictedTimeSeries.YValues[i],
			}
			for j, val := range trend.r.GetCoeffs() {
				if j == 0 {
					annotation.Label = fmt.Sprintf("Predicted %s = %e", trend.model.GetObservedName(), val)
				} else {
					annotation.Label += fmt.Sprintf(" + %s*%e", trend.r.GetVar(j-1), val)
				}
			}
			annotationSeries.Annotations = []chart.Value2{annotation}
		}
		//fmt.Println(i, times[i].Format("2006-01-02"), observedTimeSeries.YValues[i], predictedTimeSeries.YValues[i])
	}

	// Because both series used the same times, we will just copy the reference
	observedTimeSeries.XValues = times
	predictedTimeSeries.XValues = times

	graph := chart.Chart{
		Width:  width,
		Height: height,
		YAxis: chart.YAxis{
			Name:           trend.model.GetObservedName(),
			NameStyle:      chart.Style{Show: true},
			Style:          chart.Style{Show: true},
			Ascending:      true,
			TickStyle:      chart.Style{Show: true},
			Ticks:          nil,
			GridLines:      nil,
			GridMajorStyle: chart.Style{Show: true},
			GridMinorStyle: chart.Style{Show: true},
		},
		XAxis: chart.XAxis{
			Name:           trend.model.GetVariableNames()[0],
			NameStyle:      chart.Style{Show: true},
			Style:          chart.Style{Show: true},
			TickStyle:      chart.Style{Show: true},
			GridMajorStyle: chart.Style{Show: true},
			GridMinorStyle: chart.Style{Show: true},
		},
		Series: []chart.Series{
			observedTimeSeries,
			predictedTimeSeries,
			annotationSeries,
		},
	}

	buffer = bytes.NewBuffer([]byte{})
	if err = graph.Render(chart.PNG, buffer); err != nil {
		err = errors.Wrapf(err, "could not generate Trend chart for %s", reflect.TypeOf(trend.model).Elem().String())
	}
	return
}
