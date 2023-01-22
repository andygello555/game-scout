package email

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/gob"
	"fmt"
	"github.com/andygello555/game-scout/browser"
	"github.com/andygello555/game-scout/db/models"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/andygello555/gotils/v2/numbers"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	"github.com/playwright-community/playwright-go"
	"github.com/volatiletech/null/v9"
	"html/template"
	"io"
	"jaytaylor.com/html2text"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"time"
	"unicode"
)

func init() {
	gob.Register(MeasureContext{})
}

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

func (m *MeasureContext) Path() TemplatePath  { return Measure }
func (m *MeasureContext) Template() *Template { return m.Path().Template(m) }
func (m *MeasureContext) HTML() *Template     { return m.Template().HTML() }
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

//go:embed templates/*
var templates embed.FS

const templateDir = "templates/"

// TemplatePath represents the path of an HTML template in the repo. Each template that is going to be converted to PDF,
// must contain an element that implements a .container class. This is used to calculate the width of the final generated
// PDF.
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
func (tt TemplatePath) Template(context Context) *Template {
	return &Template{
		Path: tt,
		Template: template.Must(
			template.New(
				filepath.Base(tt.Path()),
			).Funcs(
				tt.Context().Funcs(),
			).ParseFS(templates, tt.Path())),
		Config:      Client.Config().EmailTemplateConfigFor(tt),
		ContentType: NotExecuted,
		Context:     context,
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
	// Config is the TemplateConfig for this template.
	Config TemplateConfig
	// Buffer is a bytes.Reader that contains the parsed output for the results produced by Template.HTML and
	// Template.PDF. This is overwritten each time.
	Buffer bytes.Reader
	// Error is the error returned by any of the chained methods.
	Error error
	// ContentType is the current TemplateBufferContentType of the Buffer.
	ContentType TemplateBufferContentType
	// Context is the Context that this Template will/has used to generate HTML from the TemplatePath.
	Context Context
}

// copyTemplate will copy the given Template to a new instance of Template. If the original Template has an error, it
// will wrap the error and overwrite it in the copied instance.
func copyTemplate(template *Template) *Template {
	output := &Template{
		Path:        template.Path,
		Template:    template.Template,
		Config:      template.Config,
		Buffer:      template.Buffer,
		Error:       template.Error,
		ContentType: template.ContentType,
		Context:     template.Context,
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

// HTML will call the Execute method on the Template.Template with the given Template.Context. Template.Error is set if:
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
func (t *Template) HTML() (output *Template) {
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

	if output.Context.Path() != output.Path {
		output.Error = fmt.Errorf(
			"%s is intended for the %s Template not the %s Template",
			reflect.TypeOf(output.Context).Elem().String(), output.Context.Path().Name(), output.Path.Name(),
		)
		return
	}

	buffer := bytes.NewBuffer([]byte{})
	if err := output.Template.Execute(buffer, output.Context); err != nil {
		output.Error = errors.Wrapf(err, "could not execute template %s with the given context", t.Path.Name())
		return
	}
	output.Buffer = *bytes.NewReader(buffer.Bytes())
	buffer = nil

	output.ContentType = HTML
	return
}

const (
	// PixelsToMM is the conversion constant for converting pixels to MM at a 96 DPI.
	PixelsToMM = 0.264583333
	PDFDPI     = 96
)

// PDF will convert a Template with the ContentType HTML to a PDF. Template.Error is set if:
//
// • The Template already contains an error.
//
// • The Template.ContentType is not HTML.
//
// • An error occurred in any of the functions used to generate the PDF.
//
// If Template.PDF can run without setting the Template.Error then Template.ContentType is set to PDF. PDF will
// calculate the size of the generated PDF by opening the HTML in a headless playwright browser, then fetch the height
// and width (in pixels) of the first element to use the .container class on the page. These values are then converted to
// MM by using the PixelsToMM conversion constant, which assumes that the PDF DPI is 96.
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

	var (
		file *os.File
		err  error
	)
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

	_, _ = output.Buffer.Seek(0, io.SeekStart)
	if _, err = output.Buffer.WriteTo(bufio.NewWriter(file)); err != nil {
		output.Error = errors.Wrap(err, "could not write HTML buffer to temporary file")
		return
	}

	var b *browser.Browser
	if b, err = browser.Chromium.Browser(true); err != nil {
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

	b.Pages[0].WaitForLoadState("domcontentloaded")

	var container playwright.ElementHandle
	if container, err = b.Pages[0].QuerySelector(".container"); err != nil {
		output.Error = errors.Wrapf(err, ".container could not be found in the temp HTML file at %s", file.Name())
		return
	}

	var (
		pageWidth  any
		pageHeight any
	)
	if pageWidth, err = container.GetProperty("offsetWidth"); err != nil {
		output.Error = errors.Wrapf(err, ".container's offsetWidth could not be found in the temp HTML file at %s", file.Name())
		return
	}
	if pageHeight, err = container.GetProperty("offsetHeight"); err != nil {
		output.Error = errors.Wrapf(err, ".container's offsetHeight could not be found in the temp HTML file at %s", file.Name())
		return
	}

	if pageWidth, err = pageWidth.(playwright.JSHandle).JSONValue(); err != nil {
		output.Error = errors.Wrap(err, ".container's width in the temp HTML file could not be JSON-ified")
		return
	}
	if pageHeight, err = pageHeight.(playwright.JSHandle).JSONValue(); err != nil {
		output.Error = errors.Wrap(err, ".container's height in the temp HTML file could not be JSON-ified")
		return
	}

	pageWidthString := fmt.Sprintf("%dpx", pageWidth)
	pageHeightString := fmt.Sprintf("%dpx", pageHeight)
	printBackground := true

	var pdfBytes []byte
	if pdfBytes, err = b.Pages[0].PDF(playwright.PagePdfOptions{
		PrintBackground: &printBackground,
		Width:           &pageWidthString,
		Height:          &pageHeightString,
	}); err != nil {
		output.Error = errors.Wrapf(err, "could not create PDF from filled HTML template %s", output.Path.Name())
		return
	}

	output.Buffer = *bytes.NewReader(pdfBytes)
	output.ContentType = PDF
	return
}

// WriteFile will write the Buffer to a file of the given filename.
func (t *Template) WriteFile(filename string) (err error) {
	var file *os.File
	if file, err = os.Create(filename); err != nil {
		return errors.Wrap(err, "could not create file to save Template buffer to")
	}
	defer func(file *os.File) {
		err = myErrors.MergeErrors(err, errors.Wrap(file.Close(), "could not close file with Template.Buffer contents"))
	}(file)

	if _, err = t.Buffer.WriteTo(file); err != nil {
		return errors.Wrap(err, "could not write Template.Buffer to file")
	}
	return
}

// Email returns the filled Email instance that can be sent using the email Client.
//
// • If the given Template has an error set in Template.Error then this error will be wrapped and returned.
//
// • If the given Template has a ContentType of HTML, then the sent email will have an HTML body, a plain-text
// body obtained from html2text.FromString, and the HTML will be added as an attachment.
//
// • If the given Template has a ContentType of PDF, then the sent email will have an HTML body obtained by executing a
// brand-new template with the Template.Context, a plain-text body obtained from html2text.FromString, and the PDF will
// be added as an attachment.
//
// • If the given Template does not have a ContentType that is either HTML or PDF, then an error will be returned.
func (t *Template) Email() (email *Email, err error) {
	if t.Error != nil {
		err = errors.Wrapf(err, "cannot create transmission for %s template as it contains an error", t.Path.Name())
		return
	}

	recipients := t.Config.TemplateTo()
	if Client.Config().EmailDebug() {
		recipients = t.Config.TemplateDebugTo()
	}

	if email, err = NewEmail(); err != nil {
		err = errors.Wrap(err, "could not create Email")
	}
	email.FromName = Client.Config().EmailFromName()
	email.FromAddress = Client.Config().EmailFrom()
	email.Recipients = recipients
	email.Subject = t.Config.TemplateSubject()

	var (
		plain            string
		htmlBuffer       *bytes.Reader
		attachmentBuffer *bytes.Reader
		attachmentName   string
	)
	parts := make([]Part, 0)
	switch t.ContentType {
	case HTML:
		htmlBuffer = &t.Buffer
		attachmentBuffer = &t.Buffer
		attachmentName = t.Config.TemplateAttachmentName() + ".html"
		if plain, err = html2text.FromReader(&t.Buffer, t.Config.TemplateHTML2TextOptions()); err != nil {
			err = errors.Wrapf(err, "cannot convert %s template to plain text from HTML", t.Path.Name())
			return
		}
	case PDF:
		htmlTemplate := t.Context.HTML()
		htmlBuffer = &htmlTemplate.Buffer
		attachmentBuffer = &t.Buffer
		attachmentName = t.Config.TemplateAttachmentName() + ".pdf"
		if plain, err = html2text.FromReader(&htmlTemplate.Buffer, t.Config.TemplateHTML2TextOptions()); err != nil {
			err = errors.Wrapf(err, "cannot convert %s template to plain text from HTML/PDF", t.Path.Name())
			return
		}
	default:
		err = fmt.Errorf(
			"%s template has content-type %s, to create a Email we need %s or %s",
			t.Path.Name(), t.ContentType.String(), HTML.String(), PDF.String(),
		)
		return
	}

	parts = append(parts,
		Part{Buffer: bytes.NewReader([]byte(plain))},
		Part{Buffer: attachmentBuffer, Attachment: true, Filename: attachmentName},
	)
	if !t.Config.TemplatePlainOnly() {
		parts = append(parts, Part{Buffer: htmlBuffer})
	}

	if err = email.AddParts(parts...); err != nil {
		err = myErrors.MergeErrors(err, email.Close())
		return
	}
	return
}

// SendAsync the Template using the default TemplateMailer (Client) with ClientWrapper.SendAsync.
func (t *Template) SendAsync() <-chan Response {
	return Client.SendAsync(t)
}

// SendSync the Template using the default TemplateMailer (Client) with ClientWrapper.SendSync.
func (t *Template) SendSync() Response {
	return Client.SendSync(t)
}
