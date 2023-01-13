package models

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"github.com/pkg/errors"
	"github.com/sajari/regression"
	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"
	"gorm.io/gorm"
	"html/template"
	"reflect"
	"time"
)

func init() {
	gob.Register(Trend{})
}

// trendModel is implemented by Model instances that need to have their trends measured by one or more metrics that
// exist as a field in the model.
type trendModel interface {
	// GetObservedName will return the name of the observed variable for the model.
	GetObservedName() string
	// GetVariableNames will return the name of the variables for the model.
	GetVariableNames() []string
	// Train will add the data points to the Trend using the Trend.AddDataPoint method.
	Train(trend *Trend) error
	// Trend will return the fully trained Trend for the model instance.
	Trend(db *gorm.DB) (*Trend, error)
}

// Trend references the structure used to calculate the regression of the observed variable for a trendModel. This will
// only work for a regression that has the timestamp of when each trendModel instance was created as a variable that
// affects the regression's predictions.
type Trend struct {
	Model      trendModel
	Regression *regression.Regression
	DataPoints regression.DataPoints
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
		Model:      model,
		Regression: &r,
		db:         db,
	}
}

// AddDataPoint will add a data point for the given date (variable) and score (observed value). It can also be given
// extra variables in case you wanted to do polynomial regression.
func (trend *Trend) AddDataPoint(date time.Time, score float64, extraVars ...float64) {
	if trend.DataPoints == nil {
		trend.DataPoints = make(regression.DataPoints, 0)
	}

	year, month, day := date.Date()
	vars := []float64{float64(time.Date(year, month, day, 0, 0, 0, 0, time.UTC).UnixNano())}
	if len(extraVars) > 0 {
		vars = append(vars, extraVars...)
	}

	dp := regression.DataPoint(score, vars)
	trend.DataPoints = append(trend.DataPoints, dp)
	trend.Regression.Train(dp)
}

// Train will add the training data of the model instance to the regression model. This will not run the training
// itself.
func (trend *Trend) Train() (err error) {
	if err = trend.Model.Train(trend); err != nil {
		err = errors.Wrapf(err, "could not Train instance of %s", reflect.TypeOf(trend.Model).Elem().String())
	}
	return
}

// Trend will run the training for the regression model and return the coefficients for the resulting regression line's
// formula.
func (trend *Trend) Trend() (coefficients []float64, err error) {
	if err = trend.Regression.Run(); err != nil {
		err = errors.Wrapf(err, "could not find Trend of %s", reflect.TypeOf(trend.Model).Elem().String())
	}
	coefficients = trend.Regression.GetCoeffs()
	return
}

// GetCoeffs returns the coefficients for the regression.Regression within this Trend.
func (trend *Trend) GetCoeffs() (coefficients []float64) { return trend.Regression.GetCoeffs() }

type ChartImage struct {
	width  int
	height int
	bytes  bytes.Buffer
}

func (ci *ChartImage) Base64() string {
	encoded := base64.StdEncoding.EncodeToString(ci.bytes.Bytes())
	return fmt.Sprintf("data:image/png;base64,%s", encoded)
}

func (ci *ChartImage) Img(alt string, style string) template.HTML {
	return template.HTML(fmt.Sprintf("<img src=\"%s\" alt=\"%s\" style=\"%s\">", ci.Base64(), alt, style))
}

func (ci *ChartImage) Bytes() []byte { return ci.bytes.Bytes() }

// Chart generates a chart for the Trend with the given size. The returned bytes.Buffer can be saved as a PNG.
func (trend *Trend) Chart(width, height int) (image *ChartImage, err error) {
	times := make([]time.Time, len(trend.DataPoints))

	// Time series for the observed variable
	observedTimeSeries := chart.TimeSeries{
		Name: fmt.Sprintf("%s over time", trend.Model.GetObservedName()),
		Style: chart.Style{
			Show:        true,
			StrokeColor: chart.GetDefaultColor(0).WithAlpha(64),
			StrokeWidth: 3,
		},
		YValues: make([]float64, len(trend.DataPoints)),
	}

	// Regression time series. Uses the predicted values at each time to construct the regression line.
	predictedTimeSeries := chart.TimeSeries{
		Name: fmt.Sprintf("Regression for %s over time", trend.Model.GetObservedName()),
		Style: chart.Style{
			Show:            true,
			StrokeColor:     drawing.ColorRed,
			StrokeDashArray: []float64{5.0, 5.0},
			StrokeWidth:     3,
		},
		YValues: make([]float64, len(trend.DataPoints)),
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
	for i, dp := range trend.DataPoints {
		times[i] = time.Unix(0, int64(dp.Variables[0]))
		observedTimeSeries.YValues[i] = dp.Observed

		var trendY float64
		if trendY, err = trend.Regression.Predict(dp.Variables); err != nil {
			err = errors.Wrapf(
				err,
				"could not predict %ss %s for %s",
				reflect.TypeOf(trend.Model).Elem().String(),
				trend.Model.GetVariableNames()[0],
				times[i].String(),
			)
		}
		predictedTimeSeries.YValues[i] = trendY

		if i == len(trend.DataPoints)/2 {
			annotation := chart.Value2{
				XValue: float64(times[i].UnixNano()),
				YValue: predictedTimeSeries.YValues[i],
			}
			for j, val := range trend.Regression.GetCoeffs() {
				if j == 0 {
					annotation.Label = fmt.Sprintf("Predicted %s = %e", trend.Model.GetObservedName(), val)
				} else {
					annotation.Label += fmt.Sprintf(" + %s*%e", trend.Regression.GetVar(j-1), val)
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
			Name:           trend.Model.GetObservedName(),
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
			Name:           trend.Model.GetVariableNames()[0],
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

	image = &ChartImage{width: width, height: height, bytes: bytes.Buffer{}}
	if err = graph.Render(chart.PNG, &image.bytes); err != nil {
		err = errors.Wrapf(err, "could not generate Trend chart for %s", reflect.TypeOf(trend.Model).Elem().String())
	}
	return
}
