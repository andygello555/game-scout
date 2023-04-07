package email

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/pkg/errors"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultBufSize                 = 4 * 1024
	compressionLimit               = 15 * 1000 * 1024
	maxAttachmentSize              = 24 * 1000 * 1024
	maxContentTypeDetectionBufSize = 512
)

var inTest = false

var (
	nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9 ]+`)
)

// plainWriteCloser is the io.WriteCloser provided by Part.Encoder for plain MIME types such as text/plain.
type plainWriteCloser struct{ *bufio.Writer }

func newPlainWriteCloser(w io.Writer) io.WriteCloser { return &plainWriteCloser{bufio.NewWriter(w)} }
func (wc *plainWriteCloser) Close() error            { return wc.Flush() }

// zipWriteCloser is the io.WriteCloser provided by Part.Encoder for any Part that has the Part.Attachment set. When
// created using newZipWriteCloser it will create a new base64 encoded archive that only contains a single file that will
// be compressed. When Close is called, the archive will be closed. Thus, the pipeline is:
//
//	Buffer -> Compressed Buffer -> Compressed Archive -> Base64 Compressed Archive -> Part File
type zipWriteCloser struct {
	io.Writer
	archive *zip.Writer
	b64     io.WriteCloser
}

func newZipWriteCloser(filename string, w io.Writer) (io.WriteCloser, error) {
	var err error
	z := &zipWriteCloser{b64: base64.NewEncoder(base64.StdEncoding, w)}
	z.archive = zip.NewWriter(z.b64)
	if z.Writer, err = z.archive.Create(filename); err != nil {
		return nil, err
	}
	return z, nil
}

func (z *zipWriteCloser) Close() (err error) {
	return myErrors.MergeErrors(z.archive.Close(), z.b64.Close())
}

// Part stores the information for a single part of an Email. When passing a Part to Email.AddPart Buffer should be set
// so that the Part can be intermittently cached to a file.
type Part struct {
	Buffer *bytes.Reader
	file   *os.File
	// ContentType can be provided, but if it is the empty string then it will be found from the first 512 bytes of the
	// Buffer using http.DetectContentType.
	ContentType string
	// Attachment should be set to true if the Part should be added as an attachment. If this is set then Filename
	// should also be set.
	Attachment bool
	// Filename is the filename of the attachment.
	Filename string
	// DropIfBig will drop the attachment from the email if its base64 encoded version larger than 25MB.
	DropIfBig bool
}

// ContentTypeSlug returns the slugified version of the ContentType that can be used within the names of the temporary
// file created for the Part.
func (p *Part) ContentTypeSlug() string {
	return nonAlphanumericRegex.ReplaceAllString(p.ContentType, "")
}

// Encoder returns an io.WriteCloser for the Part's buffer. If the Part is an Attachment, then the encoder returned will
// be a base64 encoder, anything else will be encoded as plain-text.
func (p *Part) Encoder() (io.WriteCloser, error) {
	_, _ = p.file.Seek(0, io.SeekStart)
	if p.Attachment {
		if p.Buffer.Size() > compressionLimit && p.ContentType != "application/zip" {
			p.ContentType = "application/zip"
			filename := p.Filename
			p.Filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + ".zip"
			return newZipWriteCloser(filename, p.file)
		}
		return base64.NewEncoder(base64.StdEncoding, p.file), nil
	} else {
		return newPlainWriteCloser(p.file), nil
	}
}

// Headers returns the textproto.MIMEHeader for the Part by using the ContentType.
func (p *Part) Headers() textproto.MIMEHeader {
	headers := textproto.MIMEHeader{"Content-Type": {p.ContentType}}
	if p.Attachment {
		headers["Content-Transfer-Encoding"] = []string{"base64"}
		headers["Content-Disposition"] = []string{fmt.Sprintf("attachment; filename=%s", p.Filename)}
	}
	return headers
}

// profilingKey is a key used in the Profiling type.
type profilingKey string

const (
	// ProfilingNew profiles the NewEmail procedure.
	ProfilingNew profilingKey = "NEW"
	// ProfilingAddPart profiles the Email.AddPart method.
	ProfilingAddPart profilingKey = "ADD_PART"
	// ProfilingWrite profiles the Email.Write method.
	ProfilingWrite profilingKey = "WRITE"
	// ProfilingSMTPClientCreation profiles the creation of the SMTP client in TemplateMailer.Send.
	ProfilingSMTPClientCreation profilingKey = "SMTP_CLIENT_CREATION"
	// ProfilingTLSStarted profiles the time it takes for TLS to be started in TemplateMailer.Send.
	ProfilingTLSStarted profilingKey = "TLS_STARTED"
	// ProfilingAuthAdded profiles the time it takes for AUTH to be added in TemplateMailer.Send.
	ProfilingAuthAdded profilingKey = "AUTH_ADDED"
	// ProfilingMailCommand profiles the time it takes to push the MAIL command in TemplateMailer.Send.
	ProfilingMailCommand profilingKey = "MAIL_COMMAND"
	// ProfilingRcptCommand profiles the time it takes to push a single RCPT command in TemplateMailer.Send.
	ProfilingRcptCommand profilingKey = "RCPT_COMMAND"
	// ProfilingDataCommand profiles the time it takes to push the DATA command in TemplateMailer.Send.
	ProfilingDataCommand profilingKey = "DATA_COMMAND"
	// ProfilingDataClose profiles the time it takes to close the DATA command after writing all data to the SMTP server
	// connection in TemplateMailer.Send.
	ProfilingDataClose profilingKey = "DATA_CLOSE"
	// ProfilingWriteTo profiles the Email.WriteTo method.
	ProfilingWriteTo profilingKey = "WRITE_TO"
	// ProfilingClose profiles the Email.Close method.
	ProfilingClose profilingKey = "CLOSE"
)

// string returns the string version of the profilingKey.
func (epk profilingKey) string() string { return string(epk) }

// Profiling stores all the profiling that has been done for an Email.
type Profiling map[profilingKey][]time.Duration

// start returns a function that should be called when the action corresponding to the given profilingKey has finished.
func (prof *Profiling) start(key profilingKey) func() {
	start := time.Now().UTC()
	return func() {
		if _, ok := (*prof)[key]; !ok {
			(*prof)[key] = make([]time.Duration, 0)
		}
		(*prof)[key] = append((*prof)[key], time.Now().UTC().Sub(start))
	}
}

// orderedProfile represents an element within the set of ordered profiles. An array of these is returned by the
// Profiling.ordered method.
type orderedProfile struct {
	key           profilingKey
	durations     []time.Duration
	totalDuration time.Duration
	calledTimes   int
}

// ordered returns the profiles within Profiling ordered by total duration taken for a profilingKey from shorted to
// longest. It also returns the total duration spent profiling for this Profiling.
func (prof *Profiling) ordered() (totalDuration time.Duration, profiles []orderedProfile) {
	i := 0
	profiles = make([]orderedProfile, len(*prof))
	totalDuration = time.Duration(0)
	for key, durations := range *prof {
		profile := orderedProfile{
			key:           key,
			durations:     durations,
			totalDuration: time.Duration(0),
			calledTimes:   len(durations),
		}
		for _, duration := range durations {
			profile.totalDuration += duration
		}
		totalDuration += profile.totalDuration
		profiles[i] = profile
		i++
	}

	less := func(i, j int) bool {
		return profiles[i].totalDuration < profiles[j].totalDuration
	}
	if inTest {
		less = func(i, j int) bool {
			return profiles[i].key.string() < profiles[j].key.string()
		}
	}
	sort.Slice(profiles, less)
	return
}

// Total returns the total time we spent profiling an Email.
func (prof *Profiling) Total() time.Duration {
	totalDuration, _ := prof.ordered()
	return totalDuration
}

// String returns the all the profiles that have been created within Profiling in the time-taken order.
func (prof *Profiling) String() string {
	totalDuration, profiles := prof.ordered()

	var b strings.Builder
	if !inTest {
		b.WriteString(fmt.Sprintf("Total duration: %s\n", totalDuration.String()))
	} else {
		b.WriteString("Total duration: X\n")
	}
	for j, profile := range profiles {
		if !inTest {
			b.WriteString(fmt.Sprintf(
				"%d: %s was called %d time(s) with an overall time of %s (%.2f%%)",
				j+1, profile.key.string(), profile.calledTimes, profile.totalDuration.String(),
				float64(profile.totalDuration)/float64(totalDuration)*100.0,
			))
		} else {
			b.WriteString(fmt.Sprintf(
				"%d: %s was called %d time(s) with an overall time of X (XX.XX%%)",
				j+1, profile.key.string(), profile.calledTimes,
			))
		}
		if j < len(profiles)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Email represents an email MIME multipart file that can be sent using a TemplateMailer.
//
// • First call NewEmail to construct a new Email instance.
//
// • Then set the FromAddress, FromName, Subject, and Recipients fields.
//
// • Add all the parts of the email using AddPart or AddParts. This will encode each Part using Part.Encoder and cache
// them to the disk.
//
// • Then call Write, to cache the entire MIME multipart email to disk.
//
// • You can then use either Read or ReadBuffered to read the entire MIME multipart email.
//
// • Finally, call Close to close all the Part files and the Email file as well as to remove these files from the disk.
//
// Note: the last three steps should all be performed by the TemplateMailer.Send method anyway.
type Email struct {
	// parts are all the Part within the Email. They can be added using AddPart and AddParts.
	parts []Part
	// file is the os.File pointer to the finished Email body that will be written as a MIME Multipart file.
	file *os.File
	// finalSize is the size of file in bytes after Write has been called.
	finalSize int64
	// written is whether the Email has been written to file yet using Write.
	written bool
	// closed is whether the Email's temporary file, and all parts temporary files have been closed and removed using
	// Close.
	closed bool
	// Profiling is the Profiling instance used to measure the time-of-completion for all actions related to the Email.
	Profiling Profiling
	// FromAddress is the email address of the sender.
	FromAddress string
	// FromName is the name of the sender.
	FromName string
	// Subject is the subject of the Email.
	Subject string
	// Recipients are the email addresses for the recipients of this Email.
	Recipients []string
}

// NewEmail creates a new Email instance and creates a temporary file to hold the finished multipart request.
func NewEmail() (email *Email, err error) {
	email = &Email{parts: make([]Part, 0), Profiling: make(Profiling)}
	defer email.Profiling.start(ProfilingNew)()
	if email.file, err = os.CreateTemp("", "*_email"); err != nil {
		err = errors.Wrap(err, "could not create temp file for email")
	}
	return
}

// closeTempFiles closes and deletes all the temporary Part.file as well as the Email.file.
func (e *Email) closeTempFiles() (err error) {
	partFiles := slices.Comprehension(e.parts, func(idx int, value Part, arr []Part) *os.File {
		return value.file
	})
	partFiles = append(partFiles, e.file)
	for _, file := range partFiles {
		filename := file.Name()
		if err = file.Close(); err != nil {
			err = errors.Wrapf(err, "could not close temp file %s", filename)
			return
		}

		if err = os.Remove(file.Name()); err != nil {
			err = errors.Wrapf(err, "could not remove temp file %s", filename)
			return
		}
	}
	return
}

// From returns the FromName and FromAddress in the format that the email header requires.
func (e *Email) From() string { return fmt.Sprintf("%s <%s>", e.FromName, e.FromAddress) }

// AddPart will add a Part with a Part.Buffer to the Email. This will encode the Part to its required encoding and cache
// the Part intermittently to the disk.
func (e *Email) AddPart(part Part) (err error) {
	defer e.Profiling.start(ProfilingAddPart)()
	if part.Buffer == nil {
		return fmt.Errorf("cannot add a Part that has no buffer")
	}

	// If the content type is not given, we will automatically figure out the content type
	if part.ContentType == "" {
		_, _ = part.Buffer.Seek(0, io.SeekStart)
		b := make([]byte, int(math.Min(float64(part.Buffer.Len()), maxContentTypeDetectionBufSize)))
		if _, err = part.Buffer.Read(b); err != nil {
			err = errors.Wrap(err, "could not read part buffer to byte array")
		}
		_, _ = part.Buffer.Seek(0, io.SeekStart)
		part.ContentType = http.DetectContentType(b)
	}

	// Create a temp file for the part
	if part.file, err = os.CreateTemp(
		"", fmt.Sprintf("*_%s", part.ContentTypeSlug()),
	); err != nil {
		err = errors.Wrapf(err, "could not add %s part to email", part.ContentType)
		return
	}

	// Write the buffer to the encoder stream in chunks
	var wc io.WriteCloser
	if wc, err = part.Encoder(); err != nil {
		err = errors.Wrapf(err, "could not create an encoder for part no. %d", len(e.parts)+1)
		return
	}

	reader := bufio.NewReader(part.Buffer)
	if size := reader.Size(); size > maxAttachmentSize && part.Attachment {
		if part.DropIfBig {
			return
		}
		err = fmt.Errorf("attachment %q is bigger than %d bytes, and DropIfBig is not set", part.Filename, size)
		return
	}

	nBytes, nChunks := int64(0), int64(0)
	buf := make([]byte, 0, defaultBufSize)
	for {
		var n int
		n, err = reader.Read(buf[:cap(buf)])
		buf = buf[:n]
		if n == 0 {
			if err == nil {
				continue
			}
			if err == io.EOF {
				break
			}
			return errors.Wrapf(err, "error occurred whilst reading chunk %d from buffer", nChunks)
		}
		nChunks++
		nBytes += int64(len(buf))
		if n, err = wc.Write(buf); err != nil {
			err = errors.Wrapf(
				err, "error occurred whilst writing chunk %d from buffer to part %s",
				nChunks, part.file.Name(),
			)
			return
		}
	}
	if err = wc.Close(); err != nil {
		err = errors.Wrapf(err, "error occurred whilst closing writer for part %s", part.file.Name())
		return
	}
	_, _ = part.Buffer.Seek(0, io.SeekStart)
	part.Buffer = nil

	if _, err = part.file.Seek(0, io.SeekStart); err != nil {
		err = errors.Wrapf(
			err, "could not seek to beginning of temp file %s for %s part (no. %d)",
			part.file.Name(), part.ContentType, len(e.parts)+1,
		)
		return
	}
	e.parts = append(e.parts, part)
	return
}

// AddParts will call AddPart for all provided Part.
func (e *Email) AddParts(parts ...Part) (err error) {
	for partNo, part := range parts {
		if err = e.AddPart(part); err != nil {
			err = errors.Wrapf(err, "could not add part no. %d", partNo+1)
			return
		}
	}
	return
}

// Write will construct the final Email from all the cached parts. This final multipart request will also be cached to
// disk in a temporary file.
func (e *Email) Write() (err error) {
	defer e.Profiling.start(ProfilingWrite)()
	if !e.written {
		writer := bufio.NewWriter(e.file)
		fprintfWrap := func(format string, a ...any) (err error) {
			_, err = fmt.Fprintf(writer, format, a...)
			return
		}

		mw := multipart.NewWriter(writer)
		boundary := mw.Boundary()
		// For testing purposes we replace all the boundaries with the string "BOUNDARY"
		if inTest {
			boundary = "BOUNDARY"
			_ = mw.SetBoundary(boundary)
		}

		if err = myErrors.MergeErrors(
			fprintfWrap("MIME-Version: 1.0\r\n"),
			fprintfWrap("Content-Type: multipart/alternative; boundary=%s\r\n", boundary),
			fprintfWrap("From: %s\r\n", e.From()),
			fprintfWrap("To: %s\r\n", strings.Join(e.Recipients, ",")),
			fprintfWrap("Subject: %s\r\n", e.Subject),
		); err != nil {
			err = errors.Wrapf(
				err, "could not write mail header to file %s",
				e.file.Name(),
			)
		}

		for partNo, part := range e.parts {
			var w io.Writer
			if w, err = mw.CreatePart(part.Headers()); err != nil {
				err = errors.Wrapf(err, "could not create part for part no. %d for %s", partNo+1, part.file.Name())
				return
			}

			_, _ = part.file.Seek(0, io.SeekStart)
			partR := bufio.NewReader(part.file)
			if _, err = partR.WriteTo(w); err != nil {
				err = errors.Wrapf(
					err, "could not write part no. %d (%s) to %s",
					partNo+1, part.ContentType, e.file.Name(),
				)
			}
		}

		if err = mw.Close(); err != nil {
			err = errors.Wrap(err, "could not close multipart writer")
			return
		}

		if err = writer.Flush(); err != nil {
			err = errors.Wrapf(err, "could not flush buffered writer for %s", e.file.Name())
			return
		}
		e.written = true
		stat, _ := e.file.Stat()
		e.finalSize = stat.Size()
	}
	return
}

// ReadBuffered will return a bufio.Reader for the final MIME multipart email request.
func (e *Email) ReadBuffered() *bufio.Reader {
	if e.written {
		_, _ = e.file.Seek(0, io.SeekStart)
		return bufio.NewReader(e.file)
	}
	return nil
}

// Read will return a bytes.Buffer containing all of the final MIME multipart email request.
func (e *Email) Read() *bytes.Buffer {
	if e.written {
		buf := bytes.NewBuffer([]byte{})
		reader := e.ReadBuffered()
		for {
			token, err := reader.ReadBytes('\n')
			buf.Write(token)
			if err != nil {
				break
			}
		}
		return buf
	}
	return nil
}

// WriteTo will write the Email to the given io.Writer in chunks, as to not load all the Email into memory. Write still
// needs to be called before this.
func (e *Email) WriteTo(w io.Writer) (n int64, err error) {
	defer e.Profiling.start(ProfilingWriteTo)()
	var chunk int64
	reader := e.ReadBuffered()
	buf := make([]byte, 0, defaultBufSize)
	for {
		var read int
		read, err = reader.Read(buf[:cap(buf)])
		buf = buf[:read]
		if read == 0 {
			if err == nil {
				continue
			}
			if err == io.EOF {
				err = nil
				break
			}
			err = errors.Wrapf(err, "error occurred whilst reading chunk no. %d from Email", chunk)
			return
		}
		n += int64(len(buf))
		var written int
		if written, err = w.Write(buf); err != nil {
			err = errors.Wrapf(
				err, "error occurred whilst writing chunk no. %d of size %d from Email",
				chunk, written,
			)
			return
		}
		chunk++
	}
	return
}

// Close will close and remove all the temp files created by the Email.
func (e *Email) Close() (err error) {
	defer e.Profiling.start(ProfilingClose)()
	if !e.closed {
		e.closed = true
		return e.closeTempFiles()
	}
	return fmt.Errorf("email is already closed")
}

// Size is the size of the final Email in bytes. This is equal to the size of the temporary file used to store the email
// within.
func (e *Email) Size() int64 { return e.finalSize }

// validateLine checks to see if a line has CR or LF as per RFC 5321
func validateLine(line string) error {
	if strings.ContainsAny(line, "\n\r") {
		return errors.New("smtp: A line must not contain CR or LF")
	}
	return nil
}
