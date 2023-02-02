package email

import (
	"fmt"
	"github.com/andygello555/game-scout/db/models"
	"github.com/andygello555/gotils/v2/numbers"
	"github.com/deckarep/golang-set/v2"
	"github.com/volatiletech/null/v9"
	"html/template"
	"reflect"
	"strconv"
	"time"
	"unicode"
)

// Context will be implemented by structures that are used to fill out a Template in Template.Execute.
type Context interface {
	// Path returns the TemplatePath that this Context is for.
	Path() TemplatePath
	// Template returns an un-executed Template that this Context can be used for.
	Template() *Template
	// Funcs returns the functions that should be bound to the template.Template before parsing the HTML/Text template
	// located in at the TemplatePath.
	Funcs() template.FuncMap
	// Execute executes the Context via Template.Execute.
	Execute() *Template
	// AdditionalParts returns any additional Part to add onto an Email.
	AdditionalParts() ([]Part, error)
}

// MeasureContext is a Context that contains the data required to fill out the Measure HTML template.
type MeasureContext struct {
	Start                  time.Time
	End                    time.Time
	TrendingDevs           []*models.TrendingDev
	TopSteamApps           []*models.SteamApp
	DevelopersBeingDeleted []*models.TrendingDev
	EnabledDevelopers      int64
	Config                 Config
}

func (m *MeasureContext) Path() TemplatePath               { return Measure }
func (m *MeasureContext) Execute() *Template               { return m.Template().Execute() }
func (m *MeasureContext) AdditionalParts() ([]Part, error) { return []Part{}, nil }
func (m *MeasureContext) Template() *Template              { return NewParsedTemplate(HTML, m).Template(m) }
func (m *MeasureContext) Funcs() template.FuncMap {
	return map[string]any{
		"intRange": func(start, end, step int) []int {
			return numbers.Range(start, end, step)
		},
		"contains": func(set []string, elem string) bool {
			return mapset.NewThreadUnsafeSet(set...).Contains(elem)
		},
		"timePretty": func(t time.Time) string {
			loc, _ := time.LoadLocation("Europe/London")
			t = t.In(loc)
			return numbers.Ordinal(t.Day()) + t.Format(" January 2006 at 3pm")
		},
		"datePretty": func(t time.Time) string {
			return t.Format("02/01/2006")
		},
		"percentage": func(f null.Float64) string {
			perc := f.Float64
			if !f.IsValid() {
				perc = 1.0
			}
			return fmt.Sprintf("%.2f%%", perc*100.0)
		},
		"percentageF64": func(f float64) string {
			return fmt.Sprintf("%.2f%%", f*100.0)
		},
		"cap": func(s string) string {
			r := []rune(s)
			return string(append([]rune{unicode.ToUpper(r[0])}, r[1:]...))
		},
		"yesno": func(b bool) string {
			return map[bool]string{
				true:  "yes",
				false: "no",
			}[b]
		},
		"inc": func(i int) int {
			return i + 1
		},
		"dec": func(i int) int {
			return i - 1
		},
		"div": func(a, b int) int {
			return a / b
		},
		"ord": func(num int) string {
			return numbers.OrdinalOnly(num)
		},
		"date": func(date time.Time) time.Time {
			year, month, day := date.Date()
			return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
		},
		"duration": func(duration models.NullDuration) time.Duration {
			return time.Duration(duration.Int64)
		},
		"timeSub": func(t1 time.Time, t2 time.Time) time.Duration {
			return t1.Sub(t2)
		},
		"days": func(d time.Duration) int {
			return int(d.Hours() / 24)
		},
		"lastIndex": func(a any) int {
			return reflect.ValueOf(a).Len() - 1
		},
		"trunc": func(s string, max int) string {
			lastSpaceIx := -1
			sLen := 0
			for i, r := range s {
				if unicode.IsSpace(r) {
					lastSpaceIx = i
				}
				sLen++
				if sLen >= max {
					if lastSpaceIx != -1 {
						return s[:lastSpaceIx] + "..."
					}
					// If here, string is longer than max, but has no spaces
				}
			}
			return s
		},
		"float": func(f float64) string {
			return strconv.FormatFloat(f, 'G', 12, 64)
		},
	}
}

type FinishedContext struct {
	BatchSize       int
	DiscoveryTweets int
	Started         time.Time
	Finished        time.Time
	Result          *models.ScoutResult
}

func (f *FinishedContext) Path() TemplatePath               { return Finished }
func (f *FinishedContext) Execute() *Template               { return f.Template().Execute() }
func (f *FinishedContext) Template() *Template              { return NewParsedTemplate(Text, f).Template(f) }
func (f *FinishedContext) AdditionalParts() ([]Part, error) { return []Part{}, nil }
func (f *FinishedContext) Funcs() template.FuncMap {
	return map[string]any{
		"stamp": func(t time.Time) string {
			return t.Format(time.Stamp)
		},
		"duration": func(start time.Time, end time.Time) time.Duration {
			return start.Sub(end)
		},
	}
}
