package email

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/gob"
	"fmt"
	"github.com/andygello555/game-scout/browser"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/pkg/errors"
	"github.com/playwright-community/playwright-go"
	"html/template"
	"io"
	"jaytaylor.com/html2text"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	textTemplate "text/template"
)

func init() {
	gob.Register(MeasureContext{})
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
	Started TemplatePath = templateDir + "started.txt"
	Error   TemplatePath = templateDir + "error.txt"
)

// TemplatePathFromName returns the TemplatePath of the given name.
func TemplatePathFromName(name string) TemplatePath {
	switch strings.ToLower(name) {
	case "measure":
		return Measure
	case "started":
		return Started
	case "error":
		return Error
	default:
		panic(fmt.Errorf("there is no template with name %q", name))
	}
}

// Name returns the name of the TemplatePath that is synonymous to the name of the enum constant.
func (tt TemplatePath) Name() string {
	switch tt {
	case Measure:
		return "Measure"
	case Started:
		return "Started"
	case Error:
		return "Error"
	default:
		return "Unknown"
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
	// Text is set after the Template.Execute method is called successfully on a Text Template. Text templates cannot be
	// converted to HTML or PDF.
	Text
	// HTML is set after the Template.Execute method is called successfully on an HTML Template.
	HTML
	// PDF is set after the Template.PDF method is called successfully.
	PDF
)

// String returns the formal name of the TemplateBufferContentType.
func (ct TemplateBufferContentType) String() string {
	switch ct {
	case NotExecuted:
		return "Not Executed"
	case Text:
		return "Text"
	case HTML:
		return "HTML"
	case PDF:
		return "PDF"
	default:
		return "Unknown"
	}
}

// parsedTemplate serves as a wrapper for a parsed template.Template or a parsed textTemplate.Template.
type parsedTemplate struct {
	htmlTemplate *template.Template
	textTemplate *textTemplate.Template
}

func NewParsedTemplate(contentType TemplateBufferContentType, context Context) parsedTemplate {
	switch contentType {
	case HTML:
		return parsedTemplate{
			htmlTemplate: template.Must(
				template.New(
					filepath.Base(context.Path().Path()),
				).Funcs(
					context.Funcs(),
				).ParseFS(
					templates,
					context.Path().Path(),
				),
			),
		}
	case Text:
		return parsedTemplate{
			textTemplate: textTemplate.Must(
				textTemplate.New(
					filepath.Base(context.Path().Path()),
				).Funcs(
					context.Funcs(),
				).ParseFS(
					templates,
					context.Path().Path(),
				),
			),
		}
	default:
		return parsedTemplate{}
	}
}

// Template returns an instantiated Template using the given Context.
func (p parsedTemplate) Template(context Context) *Template {
	return &Template{
		Path:        context.Path(),
		Template:    p,
		Config:      Client.Config().EmailTemplateConfigFor(context.Path()),
		ContentType: NotExecuted,
		Context:     context,
	}
}

// ExecutedContentType returns the TemplateBufferContentType that the Template.ContentType should be set to after
// Execute has been called.
func (p parsedTemplate) ExecutedContentType() TemplateBufferContentType {
	switch {
	case p.htmlTemplate != nil:
		return HTML
	case p.textTemplate != nil:
		return Text
	default:
		return NotExecuted
	}
}

// Execute will call the execute method on the set parsed template. If no template is set, then an appropriate error
// will be returned.
func (p parsedTemplate) Execute(writer io.Writer, data any) error {
	switch p.ExecutedContentType() {
	case HTML:
		return p.htmlTemplate.Execute(writer, data)
	case Text:
		return p.textTemplate.Execute(writer, data)
	default:
		return fmt.Errorf("parsedTemplate has neither htmlTemplate, or textTemplate set so cannot be executed")
	}
}

// Template is a chainable structure that represents an instantiated HTML template that is read from the given Path.
// It is worth noting that the chainable methods return copies of the original Template.
type Template struct {
	// Path is the TemplatePath where the Template was loaded from.
	Path TemplatePath
	// Template is the parsed template.Template or textTemplate.Template, loaded from the Path, which is also loaded up
	// with the functions returned by Context.Funcs method for the Context for this Template.
	Template parsedTemplate
	// Config is the TemplateConfig for this template.
	Config TemplateConfig
	// Buffer is a bytes.Reader that contains the parsed output for the results produced by Template.Execute and
	// Template.PDF. This is overwritten each time.
	Buffer bytes.Reader
	// Error is the error returned by any of the chained methods.
	Error error
	// ContentType is the current TemplateBufferContentType of the Buffer.
	ContentType TemplateBufferContentType
	// Context is the Context that this Template will/has used to generate HTML or Text from the TemplatePath.
	Context Context
}

// copyTemplate will copy the given Template to a new instance of Template. If the original Template has an error, it
// will wrap the error and overwrite it in the copied instance.
func copyTemplate(template *Template) *Template {
	output := &Template{
		Path: template.Path,
		Template: parsedTemplate{
			htmlTemplate: template.Template.htmlTemplate,
			textTemplate: template.Template.textTemplate,
		},
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

func (t *Template) checkExecutable() {
	if t.ContentType != NotExecuted {
		t.Error = fmt.Errorf(
			"%s template has content-type %s, to execute this template we need %s",
			t.Path.Name(), t.ContentType.String(), NotExecuted.String(),
		)
		return
	}

	if t.Context.Path() != t.Path {
		t.Error = fmt.Errorf(
			"%s is intended for the %s Template not the %s Template",
			reflect.TypeOf(t.Context).Elem().String(), t.Context.Path().Name(), t.Path.Name(),
		)
		return
	}
}

// Execute will call Template.Template.Execute with the given Template.Context. Template.Error is set if:
//
// • The Template already contains an error.
//
// • The Template.ContentType is not NotExecuted.
//
// • The resulting value of Context.Path does not match the Template.Path.
//
// • An error occurs whilst calling the Execute method on Template.Template.
//
// If Template.Execute can run without setting the Template.Error then Template.ContentType is set to HTML or Text
// depending on the TemplateBufferContentType returned by Template.Template.ExecutedContentType.
func (t *Template) Execute() (output *Template) {
	if output = copyTemplate(t); output.Error != nil {
		output.Error = errors.Wrapf(output.Error, "%s template cannot be executed", output.Path.Name())
		return
	}

	if output.checkExecutable(); output.Error != nil {
		return
	}

	buffer := bytes.NewBuffer([]byte{})
	if err := output.Template.Execute(buffer, output.Context); err != nil {
		output.Error = errors.Wrapf(err, "could not execute template %s with the given context", t.Path.Name())
		return
	}
	output.Buffer = *bytes.NewReader(buffer.Bytes())
	buffer = nil

	output.ContentType = output.Template.ExecutedContentType()
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
// • If the given Template has a ContentType of Text, then the sent email will have a plain-text body.
//
// • If the given Template has a ContentType of HTML, then the sent email will have an HTML body, a plain-text
// body obtained from html2text.FromString, and the HTML will be added as an attachment.
//
// • If the given Template has a ContentType of PDF, then the sent email will have an HTML body obtained by executing a
// brand-new template with the Template.Context, a plain-text body obtained from html2text.FromString, and the PDF will
// be added as an attachment.
//
// • If the given Template does not have a ContentType that is either Text, HTML or PDF, then an error will be returned.
//
// Any additional Part returned by Context.AdditionalParts will also be added.
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
	switch t.ContentType {
	case Text:
		plainBuffer := new(strings.Builder)
		if _, err = io.Copy(plainBuffer, &t.Buffer); err != nil {
			err = errors.Wrapf(err, "cannot copy bytes.Reader for %s template to strings.Builder", t.Path.Name())
			return
		}
		plain = plainBuffer.String()
	case HTML:
		htmlBuffer = &t.Buffer
		attachmentBuffer = &t.Buffer
		attachmentName = t.Config.TemplateAttachmentName() + ".html"
		if plain, err = html2text.FromReader(&t.Buffer, t.Config.TemplateHTML2TextOptions()); err != nil {
			err = errors.Wrapf(err, "cannot convert %s template to plain text from HTML", t.Path.Name())
			return
		}
	case PDF:
		htmlTemplate := t.Context.Execute()
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

	errs := make([]error, 0)
	// Add plain-text part
	errs = append(errs, email.AddPart(Part{Buffer: bytes.NewReader([]byte(plain))}))

	// Add attachment if there is one
	if attachmentBuffer != nil {
		errs = append(errs, email.AddPart(Part{Buffer: attachmentBuffer, Attachment: true, Filename: attachmentName}))
	}

	// Add HTML buffer if plain only option is not set for this template
	if !t.Config.TemplatePlainOnly() {
		errs = append(errs, email.AddPart(Part{Buffer: htmlBuffer}))
	}

	// Add any additional parts
	if additionalParts := t.Context.AdditionalParts(); len(additionalParts) > 0 {
		errs = append(errs, email.AddParts(additionalParts...))
	}

	// Merge all the errors. If this returns a non-nil error then we will Close the email
	if err = myErrors.MergeErrors(errs...); err != nil {
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

// SendAsyncAndConsume will send the Template using the default TemplateMailer (Client) with Template.SendAsync and
// will consume the response using the given function. If a Response is never returned from Template.SendAsync, then the
// consumer function will never be run.
func (t *Template) SendAsyncAndConsume(consumer func(resp Response)) {
	result := t.SendAsync()
	go func() {
		var resp Response
	breakBusy:
		for {
			select {
			case resp = <-result:
				consumer(resp)
				break breakBusy
			}
		}
		return
	}()
}
