package email

import (
	"bytes"
	"fmt"
	"github.com/ainsleyclark/go-mail/mail"
	"github.com/andygello555/game-scout/db/models"
	"github.com/g8rswimmer/go-twitter/v2"
	smtpmock "github.com/mocktools/go-smtp-mock/v2"
	"jaytaylor.com/html2text"
	"mime/multipart"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"testing"
	"time"
)

type templateConfig struct {
	MaxImageWidth        int               `json:"max_image_width"`
	MaxImageHeight       int               `json:"max_image_height"`
	DebugTo              []string          `json:"debug_to"`
	To                   []string          `json:"to"`
	SubjectFormat        string            `json:"subject_format"`
	AttachmentNameFormat string            `json:"attachment_name_format"`
	SendRetries          int               `json:"send_retries"`
	SendBackoff          string            `json:"send_backoff"`
	HTML2TextOptions     html2text.Options `json:"html2text_options"`
	PlainOnly            bool              `json:"plain_only"`
}

func (c *templateConfig) TemplateMaxImageWidth() int  { return c.MaxImageWidth }
func (c *templateConfig) TemplateMaxImageHeight() int { return c.MaxImageHeight }
func (c *templateConfig) TemplateDebugTo() []string   { return c.DebugTo }
func (c *templateConfig) TemplateTo() []string        { return c.To }
func (c *templateConfig) TemplateSubject() string {
	return time.Now().Format(c.SubjectFormat)
}
func (c *templateConfig) TemplateAttachmentName() string {
	return time.Now().Format(c.AttachmentNameFormat)
}
func (c *templateConfig) TemplateSendRetries() int { return c.SendRetries }
func (c *templateConfig) TemplateSendBackoff() (time.Duration, error) {
	return time.ParseDuration(c.SendBackoff)
}
func (c *templateConfig) TemplateHTML2TextOptions() html2text.Options { return c.HTML2TextOptions }
func (c *templateConfig) TemplatePlainOnly() bool                     { return c.PlainOnly }

type emailConfig struct {
	Debug           bool                             `json:"debug"`
	Host            string                           `json:"host"`
	Port            int                              `json:"port"`
	From            string                           `json:"from"`
	FromName        string                           `json:"from_name"`
	Password        string                           `json:"password"`
	TemplateConfigs map[TemplatePath]*templateConfig `json:"template_configs"`
}

func (c *emailConfig) EmailDebug() bool      { return c.Debug }
func (c *emailConfig) EmailHost() string     { return c.Host }
func (c *emailConfig) EmailPort() int        { return c.Port }
func (c *emailConfig) EmailFrom() string     { return c.From }
func (c *emailConfig) EmailFromName() string { return c.FromName }
func (c *emailConfig) EmailPassword() string { return c.Password }
func (c *emailConfig) EmailTemplateConfigFor(path TemplatePath) TemplateConfig {
	return c.TemplateConfigs[path]
}

var config = &emailConfig{
	Debug:    true,
	From:     "test@test.com",
	FromName: "Fake Mock",
	Password: "password",
	TemplateConfigs: map[TemplatePath]*templateConfig{
		Measure: {
			MaxImageWidth:        1024,
			MaxImageHeight:       400,
			SubjectFormat:        "Robo-scout's picks of the week 2/1/2006",
			AttachmentNameFormat: "picks_of_the_week_2006_01_02",
			DebugTo:              []string{"dest@dest.com"},
			To:                   []string{},
			SendRetries:          1,
			SendBackoff:          "30s",
			HTML2TextOptions: html2text.Options{
				OmitLinks: true,
				TextOnly:  true,
			},
			PlainOnly: true,
		},
	},
}

type exampleContext struct {
	context       Context
	expectedPlain string
	err           error
}

var examples = []exampleContext{
	{
		context: &MeasureContext{
			Start: time.Now(),
			End:   time.Now(),
			TrendingDevs: []*models.TrendingDev{
				{
					Developer: &models.Developer{
						Username: "testdev",
					},
					Snapshots: []*models.DeveloperSnapshot{
						{
							CreatedAt:                    time.Date(2009, 12, 1, 13, 0, 0, 0, time.UTC),
							Version:                      3,
							TimesHighlighted:             3,
							Tweets:                       10,
							LastTweetTime:                time.Date(2009, 11, 29, 13, 0, 0, 0, time.UTC),
							AverageDurationBetweenTweets: models.NullDurationFrom(time.Hour * 6),
							TweetsPublicMetrics: &twitter.TweetMetricsObj{
								Impressions:       0,
								URLLinkClicks:     3,
								UserProfileClicks: 5,
								Likes:             100,
								Replies:           10,
								Retweets:          5,
								Quotes:            0,
							},
							Games:                 2,
							GameWeightedScoresSum: 10000,
							WeightedScore:         12000,
						},
						{
							CreatedAt:                    time.Date(2009, 12, 5, 13, 0, 0, 0, time.UTC),
							Version:                      4,
							TimesHighlighted:             3,
							Tweets:                       10,
							LastTweetTime:                time.Date(2009, 11, 3, 13, 0, 0, 0, time.UTC),
							AverageDurationBetweenTweets: models.NullDurationFrom(time.Hour * 6),
							TweetsPublicMetrics: &twitter.TweetMetricsObj{
								Impressions:       0,
								URLLinkClicks:     3,
								UserProfileClicks: 5,
								Likes:             100,
								Replies:           10,
								Retweets:          5,
								Quotes:            0,
							},
							Games:                 2,
							GameWeightedScoresSum: 11000,
							WeightedScore:         13000,
						},
					},
					Games: []*models.Game{},
				},
			},
			TopSteamApps: nil,
			Config:       config,
		},
		expectedPlain: fmt.Sprintf(`Robo-scout's picks of the week
%s - %s
Contents.

Top trending developers on Twitter @testdev.

Trending developers on Twitter.

@testdev.

I have created 2 snapshots for @testdev over a period of 4 days.

First snapshot was created on 2009-12-01.
Tweets captured
10 total.
Average duration between each tweet
6h0m0s.
Number of games captured by snapshot
2.
Total weighted score for each game
10000.
Weighted score for this snapshot
12000.

Latest snapshot was created on 2009-12-05.
Tweets captured
10 total.
Average duration between each tweet
6h0m0s.
Number of games captured by snapshot
2.
Total weighted score for each game
11000.
Weighted score for this snapshot
13000.`, time.Now().Format("02/01/2006"), time.Now().Format("02/01/2006")),
	},
}

type testClient struct {
	config    Config
	send      func(t *mail.Transmission) (mail.Response, error)
	sendAsync func(template *Template) <-chan Response
	sendSync  func(template *Template) (mail.Response, error)
}

func (tc *testClient) Config() Config                                   { return tc.config }
func (tc *testClient) Send(t *mail.Transmission) (mail.Response, error) { return tc.send(t) }
func (tc *testClient) SendAsync(template *Template) <-chan Response     { return tc.sendAsync(template) }
func (tc *testClient) SendSync(template *Template) (mail.Response, error) {
	return tc.sendSync(template)
}

func bytesFromTransmission(t *mail.Transmission) []byte {
	buf := bytes.NewBuffer(nil)

	for k, v := range t.Headers {
		buf.WriteString(fmt.Sprintf("%s: %s\n", k, v))
	}

	buf.WriteString("MIME-Version: 1.0\n")
	writer := multipart.NewWriter(buf)
	boundary := writer.Boundary()

	if t.HasAttachments() {
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s\r\n", boundary))
	} else {
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	}

	buf.WriteString(fmt.Sprintf("Subject: %s\n", t.Subject))
	buf.WriteString(fmt.Sprintf("To: %s\n", strings.Join(t.Recipients, ",")))

	if t.HasCC() {
		buf.WriteString(fmt.Sprintf("CC: %s\n", strings.Join(t.CC, ",")))
	}

	if t.PlainText != "" {
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		buf.WriteString(fmt.Sprintf("\r\n%s\r\n", strings.TrimSpace(t.PlainText)))
	}

	if t.HTML != "" {
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString(fmt.Sprintf("\r\n%s\r\n", t.HTML))
	}

	if t.HasAttachments() {
		for _, v := range t.Attachments {
			buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
			buf.WriteString(fmt.Sprintf("Content-Type: %s\n", v.Mime()))
			buf.WriteString("Content-Transfer-Encoding: base64\n")
			buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=%s\n", v.Filename))
			buf.WriteString(fmt.Sprintf("\r\n--%s", v.B64()))
		}
		buf.WriteString("--")
	}

	return buf.Bytes()
}

func fakeClientServer(t *testing.T) (server *smtpmock.Server, close func()) {
	server = smtpmock.New(smtpmock.ConfigurationAttr{
		LogToStdout:       testing.Verbose(),
		LogServerActivity: testing.Verbose(),
	})
	mail.Debug = testing.Verbose()

	// To start server use Start() method
	if err := server.Start(); err != nil {
		t.Errorf("Error occurred whilst starting mock SMTP server: %v", err)
	}
	close = func() {
		if err := server.Stop(); err != nil {
			t.Errorf("Error occurred whilst stopping mock SMTP server: %v", err)
		}
	}

	// Server's port will be assigned dynamically after server.Start()
	// for case when portNumber wasn't specified
	config.Host, config.Port = "127.0.0.1", server.PortNumber()

	// Create an email client that doesn't provide any auth when sending emails.
	client := &testClient{
		config: config,
		send: func(t *mail.Transmission) (mail.Response, error) {
			err := t.Validate()
			if err != nil {
				return mail.Response{}, err
			}

			err = smtp.SendMail(config.Host+":"+strconv.Itoa(config.Port), nil, config.From, t.Recipients, bytesFromTransmission(t))
			if err != nil {
				return mail.Response{}, err
			}

			return mail.Response{
				StatusCode: http.StatusOK,
				Message:    "Email sent successfully",
			}, nil
		},
	}
	client.sendAsync = func(template *Template) <-chan Response { return sendAsync(client, template) }
	client.sendSync = func(template *Template) (mail.Response, error) { return sendSync(client, template) }
	Client = client
	return
}

func TestMeasureContext_Template(t *testing.T) {
	_, serverClose := fakeClientServer(t)
	defer serverClose()

	var err error
	for testNo, test := range examples {
		testNo++
		template := test.context.HTML()
		if err = template.Error; err != nil {
			if test.err != nil {
				if test.err.Error() != err.Error() {
					t.Errorf("Expected error: %v for test no. %d, but got: %v", test.err, testNo, err)
				}
			} else {
				t.Errorf("Expected no error for test no. %d, but got: %v", testNo, err)
			}
		} else {
			var plain string
			if plain, err = html2text.FromString(template.Buffer.String(), template.Config.TemplateHTML2TextOptions()); err != nil {
				t.Errorf(
					"Unexpected error occurred whilst generating plain-text from %s template: %v",
					test.context.Path().Name(), err,
				)
			} else {
				if test.expectedPlain != plain {
					t.Errorf("Expected plain-text does not match actual plain-text")
				}
			}
		}
	}
}

func checkResponseError(t *testing.T, testNo int, test exampleContext, err error) {
	if err != nil {
		if test.err != nil {
			if test.err.Error() != err.Error() {
				t.Errorf("Expected error: %v for test no. %d, but got: %v", test.err, testNo, err)
			}
		} else {
			t.Errorf("Expected no error for test no. %d, but got: %v", testNo, err)
		}
	}
}

func checkMessageRequest(t *testing.T, testNo int, test exampleContext, expectedNoMessages int, server *smtpmock.Server) {
	messages := server.Messages()
	if len(messages) != expectedNoMessages {
		t.Errorf(
			"Expected there to be %d message processed by the server for test no. %d but there are %d",
			expectedNoMessages, testNo, len(messages),
		)
	} else {
		for msgNo, message := range messages {
			if !strings.Contains(strings.ReplaceAll(message.MsgRequest(), "\r", ""), test.expectedPlain) {
				t.Errorf(
					"The request for msg no. %d for test no. %d does not contain the expected plain-text",
					msgNo+1, testNo,
				)
			}
			if message.MsgResponse() != "250 Received" {
				t.Errorf(
					"The response for msg no. %d for test no. %d is not \"250 Received\" it is %s",
					msgNo+1, testNo, message.MsgResponse(),
				)
			}
		}
	}
}

func TestTemplate_SendAsync(t *testing.T) {
	server, serverClose := fakeClientServer(t)
	defer serverClose()

	sendAsyncTemplate := func(template *Template) (resp Response) {
		result := template.SendAsync()
	gotResult:
		for {
			select {
			case resp = <-result:
				break gotResult
			}
		}
		return
	}

	for testNo, test := range examples {
		testNo++

		template := test.context.HTML()
		resp := sendAsyncTemplate(template)
		checkResponseError(t, testNo, test, resp.Error)
		checkMessageRequest(t, testNo, test, 1, server)

		template = template.PDF()
		resp = sendAsyncTemplate(template)
		checkResponseError(t, testNo, test, resp.Error)
		checkMessageRequest(t, testNo, test, 2, server)
	}
}

func TestTemplate_SendSync(t *testing.T) {
	server, serverClose := fakeClientServer(t)
	defer serverClose()

	for testNo, test := range examples {
		testNo++

		template := test.context.HTML()
		_, err := template.SendSync()
		checkResponseError(t, testNo, test, err)
		checkMessageRequest(t, testNo, test, 1, server)

		template = template.PDF()
		_, err = template.SendSync()
		checkResponseError(t, testNo, test, err)
		checkMessageRequest(t, testNo, test, 2, server)
	}
}
