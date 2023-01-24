package email

import (
	"crypto/tls"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/pkg/errors"
	"io"
	"jaytaylor.com/html2text"
	"net/smtp"
	"time"
)

type TemplateMailer interface {
	Send(email *Email) (err error)
	SendAsync(template *Template) <-chan Response
	SendSync(template *Template) Response
	Auth() smtp.Auth
	Config() Config
}

var Client TemplateMailer

type TemplateConfig interface {
	TemplateMaxImageWidth() int
	TemplateMaxImageHeight() int
	TemplateDebugTo() []string
	TemplateTo() []string
	TemplateSendRetries() int
	TemplateSendBackoff() (time.Duration, error)
	TemplateSubject() string
	TemplateAttachmentName() string
	TemplateHTML2TextOptions() html2text.Options
	TemplatePlainOnly() bool
	TemplateSendDay() time.Weekday
}

type Config interface {
	EmailDebug() bool
	EmailHost() string
	EmailPort() int
	EmailAddress() string
	EmailFrom() string
	EmailFromName() string
	EmailPassword() string
	EmailTemplateConfigFor(path TemplatePath) TemplateConfig
}

type Response struct {
	Email *Email
	Error error
}

type ClientWrapper struct {
	auth   smtp.Auth
	config Config
}

func ClientCreate(config Config) (err error) {
	client := &ClientWrapper{
		auth:   smtp.PlainAuth("", config.EmailFrom(), config.EmailPassword(), config.EmailHost()),
		config: config,
	}
	Client = client
	return
}

func (c *ClientWrapper) Config() Config  { return c.config }
func (c *ClientWrapper) Auth() smtp.Auth { return c.auth }

func send(mailer TemplateMailer, email *Email) (err error) {
	// Validate sender line
	if err = validateLine(email.FromAddress); err != nil {
		return errors.Wrapf(err, "from address %q is not valid", email.FromAddress)
	}

	// Validate recipient lines
	for _, recipient := range email.Recipients {
		if err = validateLine(recipient); err != nil {
			return errors.Wrapf(err, "recipient %q is not valid", recipient)
		}
	}

	// Write the email to a temp file
	if err = email.Write(); err != nil {
		return errors.Wrap(err, "could not write Email to temp file")
	}

	// Create the SMTP client by TCP dialing to the host
	end := email.Profiling.start(ProfilingSMTPClientCreation)
	var client *smtp.Client
	if client, err = smtp.Dial(mailer.Config().EmailAddress()); err != nil {
		return errors.Wrap(err, "could not create SMTP client")
	}
	end()
	defer client.Close()

	// Check if we need to be using TLS, if so StartTLS
	end = email.Profiling.start(ProfilingTLSStarted)
	if ok, _ := client.Extension("STARTTLS"); ok {
		config := &tls.Config{ServerName: mailer.Config().EmailHost()}
		if err = client.StartTLS(config); err != nil {
			return errors.Wrap(err, "could not STARTTLS")
		}
	}
	end()

	// Add the auth, if necessary
	end = email.Profiling.start(ProfilingAuthAdded)
	if mailer.Auth() != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err = client.Auth(mailer.Auth()); err != nil {
			return errors.Wrap(err, "could not push AUTH command")
		}
	}
	end()

	// Issue MAIL command to inform server of email sender
	end = email.Profiling.start(ProfilingMailCommand)
	if err = client.Mail(email.FromAddress); err != nil {
		return errors.Wrapf(err, "could not push MAIL command for %q", email.FromAddress)
	}
	end()

	// Issue the RCPT command to inform the server of all the recipients of this email
	for _, addr := range email.Recipients {
		end = email.Profiling.start(ProfilingRcptCommand)
		if err = client.Rcpt(addr); err != nil {
			return errors.Wrapf(err, "could not push RCPT command for %q", addr)
		}
		end()
	}

	// Issue the DATA command to start writing the body of the email to the server connection
	end = email.Profiling.start(ProfilingDataCommand)
	var wc io.WriteCloser
	if wc, err = client.Data(); err != nil {
		return errors.Wrap(err, "could not start DATA command")
	}
	end()

	// We will write the entire email in <CR><LF> chunks so that we don't consume the whole email to memory
	if _, err = email.WriteTo(wc); err != nil {
		return errors.Wrap(err, "could not write Email to DATA command")
	}

	// Close the WriteCloser to flush all of the unwritten email body to the server connection
	end = email.Profiling.start(ProfilingDataClose)
	if err = wc.Close(); err != nil {
		return errors.Wrap(err, "could not close DATA command")
	}
	end()

	// Close the email after it has successfully been written to the server connection
	if err = email.Close(); err != nil {
		return errors.Wrap(err, "could not close Email")
	}

	// Finally, close the SMTP connection to the mail server
	return errors.Wrap(client.Quit(), "could not push QUIT command")
}

func (c *ClientWrapper) Send(email *Email) (err error) {
	return send(c, email)
}

func sendAsync(mailer TemplateMailer, template *Template) <-chan Response {
	result := make(chan Response, 1)
	go func(result chan<- Response) {
		resp := Response{}
		backoff, err1 := template.Config.TemplateSendBackoff()
		transmission, err2 := template.Email()
		resp.Error = myErrors.MergeErrors(err1, err2)
		resp.Email = transmission

		if resp.Error != nil {
			result <- resp
		} else {
			resp.Error = fmt.Errorf("")
			tries := 0
			for resp.Error != nil && tries < template.Config.TemplateSendRetries() {
				if resp.Error = mailer.Send(transmission); resp.Error != nil {
					if tries+1 == template.Config.TemplateSendRetries() {
						log.ERROR.Printf(
							"Could not send email \"%s\": %v, this is the last try (%d/%d) so we will not sleep",
							transmission.Subject, resp.Error, tries+1, template.Config.TemplateSendRetries(),
						)
					} else {
						sleepDuration := time.Duration(tries+1) * backoff
						log.ERROR.Printf(
							"Could not send email \"%s\": %v, try %d/%d sleeping for %s",
							transmission.Subject, resp.Error, tries+1, template.Config.TemplateSendRetries(),
							sleepDuration.String(),
						)
						time.Sleep(sleepDuration)
					}
				}
				tries++
			}
			result <- resp
		}
	}(result)
	return result
}

func (c *ClientWrapper) SendAsync(template *Template) <-chan Response {
	return sendAsync(c, template)
}

func sendSync(mailer TemplateMailer, template *Template) (resp Response) {
	result := mailer.SendAsync(template)
breakBusy:
	for {
		select {
		case resp = <-result:
			break breakBusy
		}
	}
	return
}

func (c *ClientWrapper) SendSync(template *Template) Response {
	return sendSync(c, template)
}
