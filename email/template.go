package email

import (
	"bytes"
	"embed"
	"fmt"
	"github.com/SebastiaanKlippert/go-wkhtmltopdf"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db/models"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/aws/aws-sdk-go/private/util"
	"github.com/pkg/errors"
	"github.com/volatiletech/null/v9"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

// Context will be implemented by structures that are used to fill out a Template in Template.HTML.
type Context interface {
	// Path returns the TemplatePath that this Context is for.
	Path() TemplatePath
	// Template returns an un-executed Template that this Context can be used for.
	Template() *Template
	// Funcs returns the functions that should be bound to the template.Template before parsing the HTML template in
	// located in at the TemplatePath.
	Funcs() template.FuncMap
	// HTML acts as a wrapper for Template.HTML.
	HTML() *Template
}

type TrendingDev struct {
	Developer *models.Developer
	Snapshots []*models.DeveloperSnapshot
	Games     []*models.Game
	Trend     *models.Trend
}

// MeasureContext is a Context that contains the data required to fill out the Measure HTML template.
type MeasureContext struct {
	TrendingDevs      []*TrendingDev
	TopSteamApps      []*models.SteamApp
	EnabledDevelopers int
	Config            Config
}

func (m *MeasureContext) Path() TemplatePath  { return Measure }
func (m *MeasureContext) Template() *Template { return m.Path().Template() }
func (m *MeasureContext) HTML() *Template     { return m.Template().HTML(m) }
func (m *MeasureContext) Funcs() template.FuncMap {
	return map[string]any{
		"percentage": func(f null.Float64) string {
			perc := f.Float64
			if !f.IsValid() {
				perc = 1.0
			}
			return fmt.Sprintf("%.2f%%", perc*100.0)
		},
		"cap": func(s string) string {
			return util.Capitalize(s)
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
		"ord": func(num int) string {
			var ordinalDictionary = map[int]string{
				0: "th",
				1: "st",
				2: "nd",
				3: "rd",
				4: "th",
				5: "th",
				6: "th",
				7: "th",
				8: "th",
				9: "th",
			}

			// math.Abs() is to convert negative number to positive
			floatNum := math.Abs(float64(num))
			positiveNum := int(floatNum)

			if ((positiveNum % 100) >= 11) && ((positiveNum % 100) <= 13) {
				return "th"
			}

			return ordinalDictionary[positiveNum]
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
	}
}

//go:embed templates/*
var templates embed.FS

const templateDir = "templates/"

// TemplatePath represents the path of an HTML template in the repo.
type TemplatePath string

const (
	Measure TemplatePath = templateDir + "measure.html"
)

// Name returns the name of the TemplatePath that is synonymous to the name of the enum constant.
func (tt TemplatePath) Name() string {
	switch tt {
	case Measure:
		return "Measure"
	default:
		return "Unknown"
	}
}

// Context returns an empty Context that can be used for a Template of TemplatePath.
func (tt TemplatePath) Context() Context {
	switch tt {
	case Measure:
		return &MeasureContext{}
	default:
		return nil
	}
}

// Template returns a Template that is ready to be filled using Template.HTML.
func (tt TemplatePath) Template() *Template {
	return &Template{
		Path: tt,
		Template: template.Must(
			template.New(
				filepath.Base(tt.Path()),
			).Funcs(
				tt.Context().Funcs(),
			).ParseFS(templates, tt.Path())),
		ContentType: NotExecuted,
	}
}

// Path returns the string value of the TemplatePath.
func (tt TemplatePath) Path() string { return string(tt) }

// TemplateBufferContentType is an enum representing the possible content-types of the Template.Buffer.
type TemplateBufferContentType int

const (
	// NotExecuted is the TemplateBufferContentType that is initially given to a Template constructed by
	// TemplatePath.Template.
	NotExecuted TemplateBufferContentType = iota
	// HTML is set after the Template.HTML method is called successfully.
	HTML
	// PDF is set after the Template.PDF method is called successfully.
	PDF
)

// String returns the formal name of the TemplateBufferContentType.
func (ct TemplateBufferContentType) String() string {
	switch ct {
	case NotExecuted:
		return "Not Executed"
	case HTML:
		return "HTML"
	case PDF:
		return "PDF"
	default:
		return "Unknown"
	}
}

// Template is a chainable structure that represents an instantiated HTML template that is read from the given Path.
// It is worth noting that the chainable methods return copies of the original Template.
type Template struct {
	// Path is the TemplatePath where the Template was loaded from.
	Path TemplatePath
	// Template is the parsed template.Template, loaded from the Path, which is also loaded up with the functions
	// returned by Context.Funcs method for the Context for this Template.
	Template *template.Template
	// Buffer is a byte buffer that contains the parsed output for the results produced by Template.HTML and
	// Template.PDF. This is overwritten each time.
	Buffer bytes.Buffer
	// Error is the error returned by any of the chained methods.
	Error error
	// ContentType is the current TemplateBufferContentType of the Buffer.
	ContentType TemplateBufferContentType
}

// copyTemplate will copy the given Template to a new instance of Template. If the original Template has an error, it
// will wrap the error and overwrite it in the copied instance.
func copyTemplate(template *Template) *Template {
	output := &Template{
		Path:        template.Path,
		Template:    template.Template,
		Buffer:      template.Buffer,
		Error:       template.Error,
		ContentType: template.ContentType,
	}

	if output.Error != nil {
		output.Error = errors.Wrapf(
			output.Error,
			"%s template cannot be copied as it has an error",
			output.Path.Name(),
		)
	}
	return output
}

// HTML will call the Execute method on the Template.Template with the given Context. Template.Error is set if:
//
// • The Template already contains an error.
//
// • The Template.ContentType is not NotExecuted.
//
// • The resulting value of Context.Path does not match the Template.Path.
//
// • An error occurs whilst calling the Execute method on Template.Template.
//
// If Template.HTML can run without setting the Template.Error then Template.ContentType is set to HTML.
func (t *Template) HTML(context Context) (output *Template) {
	if output = copyTemplate(t); output.Error != nil {
		output.Error = errors.Wrapf(output.Error, "%s template cannot be executed", output.Path.Name())
		return
	}

	if output.ContentType != NotExecuted {
		output.Error = fmt.Errorf(
			"%s template has content-type %s, to execute this template we need %s",
			output.Path.Name(), output.ContentType.String(), NotExecuted.String(),
		)
		return
	}

	if context.Path() != output.Path {
		output.Error = fmt.Errorf(
			"%s is intended for the %s Template not the %s Template",
			reflect.TypeOf(context).Elem().String(), context.Path().Name(), output.Path.Name(),
		)
		return
	}

	if err := t.Template.Execute(&output.Buffer, context); err != nil {
		output.Error = errors.Wrapf(err, "could not execute template %s with the given context", t.Path.Name())
		return
	}

	output.ContentType = HTML
	return
}

const (
	// PixelsToMM is the conversion constant for converting pixels to MM at a 96 DPI.
	PixelsToMM  = 0.264583333
	PDFPageZoom = 1.6
	PDFDPI      = 96
)

// PDF will convert a Template with the ContentType HTML to a PDF. Template.Error is set if:
//
// • The Template already contains an error.
//
// • The Template.ContentType is not HTML.
//
// • An error occurred in any of the functions used to generate the PDF.
//
// If Template.PDF can run without setting the Template.Error then Template.ContentType is set to PDF.
func (t *Template) PDF() (output *Template) {
	if output = copyTemplate(t); output.Error != nil {
		output.Error = errors.Wrapf(output.Error, "%s template cannot be converted to PDF", output.Path.Name())
		return
	}

	if output.ContentType != HTML {
		output.Error = fmt.Errorf(
			"%s template has content-type %s, to execute this template we need %s",
			output.Path.Name(), output.ContentType.String(), HTML.String(),
		)
		return
	}

	// Initialise a wkhtmltopdf generator
	pdf, err := wkhtmltopdf.NewPDFGenerator()
	if err != nil {
		output.Error = errors.Wrapf(err, "could not create PDF generator for %s", output.Path.Name())
		return
	}

	// read the HTML page as a PDF page
	page := wkhtmltopdf.NewPageReader(bytes.NewReader(output.Buffer.Bytes()))

	// enable this if the HTML file contains local references such as images, CSS, etc.
	page.EnableLocalFileAccess.Set(true)
	page.Zoom.Set(PDFPageZoom)

	// add the page to your generator
	pdf.AddPage(page)

	// manipulate page attributes as needed
	pdf.MarginLeft.Set(0)
	pdf.MarginRight.Set(0)
	pdf.MarginBottom.Set(0)
	pdf.MarginTop.Set(0)
	pdf.Dpi.Set(PDFDPI)

	var file *os.File
	if file, err = os.CreateTemp("", "*.html"); err != nil {
		output.Error = errors.Wrapf(err, "could not create temporary file for HTML buffer")
		return
	}
	defer func(name string) {
		output.Error = myErrors.MergeErrors(
			output.Error,
			errors.Wrap(os.Remove(name), "could not remove temporary HTML file"),
		)
	}(file.Name())

	if _, err = file.Write(output.Buffer.Bytes()); err != nil {
		output.Error = errors.Wrap(err, "could not write HTML buffer to temporary file")
		return
	}

	var b *browser.Browser
	if b, err = browser.NewBrowser(true); err != nil {
		output.Error = errors.Wrap(err, "could not open HTML file in playwright to work out page height")
		return
	}
	defer func(b *browser.Browser) {
		output.Error = myErrors.MergeErrors(
			output.Error,
			errors.Wrap(b.Quit(), "could not close browser viewing temp HTML file"),
		)
	}(b)

	if _, err = b.Pages[0].Goto(fmt.Sprintf("file://%s", file.Name())); err != nil {
		output.Error = errors.Wrapf(err, "playwright could not goto temp HTML file at file://%s", file.Name())
		return
	}

	var pageHeight any
	if pageHeight, err = b.Pages[0].Evaluate(`Math.max(
	document.body.scrollHeight, document.body.offsetHeight, document.documentElement.clientHeight,
	document.documentElement.scrollHeight, document.documentElement.offsetHeight
);`); err != nil {
		output.Error = errors.Wrap(err, "playwright could not find temp HTML file's page height")
		return
	}

	fmt.Printf("%v (%s)\n", pageHeight, reflect.TypeOf(pageHeight).String())
	pdf.PageWidth.Set(210)
	pdf.PageHeight.Set(uint(PixelsToMM * float64(pageHeight.(int)) * PDFPageZoom))

	// Create the PDF using the PDF generator
	fmt.Println(pdf.ArgString())
	err = pdf.Create()
	if err != nil {
		output.Error = errors.Wrapf(err, "could not create PDF for %s", output.Path.Name())
		return
	}

	output.Buffer = *bytes.NewBuffer(pdf.Bytes())
	output.ContentType = PDF
	return
}
