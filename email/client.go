package email

import (
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/ainsleyclark/go-mail/drivers"
	"github.com/ainsleyclark/go-mail/mail"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/pkg/errors"
	"jaytaylor.com/html2text"
	"time"
)

type TemplateMailer interface {
	mail.Mailer
	SendAsync(template *Template) <-chan Response
	SendSync(template *Template) (mail.Response, error)
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
}

type Config interface {
	EmailDebug() bool
	EmailHost() string
	EmailPort() int
	EmailFrom() string
	EmailFromName() string
	EmailPassword() string
	EmailTemplateConfigFor(path TemplatePath) TemplateConfig
}

type Response struct {
	Error error
	mail.Response
}

type ClientWrapper struct {
	auth   mail.Config
	driver mail.Mailer
	config Config
}

func ClientCreate(config Config) (err error) {
	client := &ClientWrapper{
		auth: mail.Config{
			URL:         config.EmailHost(),
			FromAddress: config.EmailFrom(),
			FromName:    config.EmailFromName(),
			Password:    config.EmailPassword(),
			Port:        config.EmailPort(),
		},
		config: config,
	}
	if client.driver, err = drivers.NewSMTP(client.auth); err != nil {
		err = errors.Wrap(err, "cannot create email ClientWrapper")
	}
	Client = client
	return
}

func (c *ClientWrapper) Config() Config { return c.config }

func (c *ClientWrapper) Send(transmission *mail.Transmission) (mail.Response, error) {
	return c.driver.Send(transmission)
}

func sendAsync(mailer TemplateMailer, template *Template) <-chan Response {
	result := make(chan Response, 1)
	go func(result chan<- Response) {
		resp := Response{}
		backoff, err1 := template.Config.TemplateSendBackoff()
		transmission, err2 := template.Transmission()
		err := myErrors.MergeErrors(err1, err2)

		if err != nil {
			resp.Error = err
			result <- resp
		} else {
			resp.Error = fmt.Errorf("")
			tries := 0
			for resp.Error != nil && tries < template.Config.TemplateSendRetries() {
				if resp.Response, resp.Error = mailer.Send(transmission); resp.Error != nil {
					sleepDuration := time.Duration(tries+1) * backoff
					log.ERROR.Printf(
						"Could not send email \"%s\": %v, sleeping for %s",
						transmission.Subject, resp.Error, sleepDuration.String(),
					)
					time.Sleep(sleepDuration)
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

func sendSync(mailer TemplateMailer, template *Template) (resp mail.Response, err error) {
	result := mailer.SendAsync(template)
breakBusy:
	for {
		select {
		case r := <-result:
			resp = r.Response
			err = r.Error
			break breakBusy
		}
	}
	return
}

func (c *ClientWrapper) SendSync(template *Template) (resp mail.Response, err error) {
	return sendSync(c, template)
}
