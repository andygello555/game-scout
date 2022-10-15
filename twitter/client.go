package twitter

import (
	"context"
	"fmt"
	"github.com/andygello555/game-scout/browser"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ClientWrapper struct {
	Client    *twitter.Client
	Config    Config
	Mutex     sync.Mutex
	RateLimit *twitter.RateLimit
	TweetCap  *TweetCap
}

var Client *ClientWrapper

type authorize struct {
	Token string
}

func (a authorize) Add(req *http.Request) {
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.Token))
}

type Config interface {
	TwitterAPIKey() string
	TwitterAPIKeySecret() string
	TwitterBearerToken() string
	TwitterUsername() string
	TwitterPassword() string
	TwitterHashtags() []string
}

func ClientCreate(config Config) error {
	Client = &ClientWrapper{
		Client: &twitter.Client{
			Authorizer: authorize{
				Token: config.TwitterBearerToken(),
			},
			Client: http.DefaultClient,
			Host:   "https://api.twitter.com",
		},
		Config:    config,
		RateLimit: nil,
	}
	if err := Client.GetTweetCap(); err != nil {
		return errors.Wrap(err, "could not create client, could not get Tweet cap")
	}
	return nil
}

// TweetCap represents the overall Tweet cap for a Twitter API app.
type TweetCap struct {
	Used      int64
	Remaining int64
	Total     int64
	Resets    time.Time
}

// String returns the string representation of the Tweet cap in the format:
// "<Used>/<Total> (<Remaining> rem.) resets: <Resets>".
func (tc *TweetCap) String() string {
	return fmt.Sprintf("%d/%d (%d rem.) resets: %s", tc.Used, tc.Total, tc.Remaining, tc.Resets.String())
}

// GetTweetCap will log in to the Twitter developer portal using browser.Browser and parse the details on the dashboard
// to a TweetCap instance.
func (w *ClientWrapper) GetTweetCap() (err error) {
	var b *browser.Browser
	var tweetCap *TweetCap
	if b, err = browser.NewBrowser(true); err != nil {
		return errors.Wrap(err, "could not create browser to get TweetCap")
	}
	var outputs []string
	if outputs, err = b.Flow(0,
		browser.Command{
			Type:  browser.Goto,
			Value: "https://developer.twitter.com/en/portal/dashboard",
		},
		browser.Command{
			Type:  browser.Select,
			Value: "input[autocomplete=username]",
			Wait:  true,
		},
		browser.Command{
			Type:  browser.Type,
			Value: w.Config.TwitterUsername(),
		},
		browser.Command{
			Type:  browser.SelectAll,
			Value: "div[role=button]",
		},
		browser.Command{
			Type: browser.FindInnerText,
			FindFunc: func(element any) bool {
				return strings.Contains(element.(string), "Next")
			},
		},
		browser.Command{
			Type: browser.Click,
		},
		browser.Command{
			Type:     browser.Select,
			Value:    "input[data-testid=ocfEnterTextTextInput]",
			Optional: true,
			Wait:     true,
		},
		browser.Command{
			Type:     browser.Type,
			Value:    strings.Split(w.Config.TwitterUsername(), "@")[0],
			Optional: true,
		},
		browser.Command{
			Type:     browser.Select,
			Value:    "div[data-testid=ocfEnterTextNextButton]",
			Wait:     true,
			Optional: true,
		},
		browser.Command{
			Type: browser.Click,
		},
		browser.Command{
			Type:  browser.Select,
			Value: "input[autocomplete=current-password]",
			Wait:  true,
		},
		browser.Command{
			Type:  browser.Type,
			Value: w.Config.TwitterPassword(),
		},
		browser.Command{
			Type:  browser.SelectAll,
			Value: "div[role=button]",
		},
		browser.Command{
			Type: browser.FindInnerText,
			FindFunc: func(element any) bool {
				return strings.Contains(element.(string), "Log in")
			},
		},
		browser.Command{
			Type: browser.Click,
		},
		browser.Command{
			Type:  browser.Select,
			Value: "div[class*=index__messageContainer]",
			Wait:  true,
		},
		browser.Command{
			Type: browser.InnerText,
		},
	); err != nil {
		return errors.Wrap(err, "could not execute Flow successfully to get TweetCap")
	}
	if err = b.Quit(); err != nil {
		return errors.Wrap(err, "could not quit Browser successfully after getting TweetCap")
	}

	tweetCap = &TweetCap{}
	tweetCapString := outputs[0]
	var used, total, month, t, tz string
	var day int
	if _, err = fmt.Sscanf(tweetCapString, "%s Tweets pulled of %s\nResets on %s %d at %s %s", &used, &total, &month, &day, &t, &tz); err != nil {
		return errors.Wrap(err, fmt.Sprintf("could not parse Tweet Cap string \"%s\"", tweetCapString))
	}
	tweetCap.Used, _ = strconv.ParseInt(strings.Replace(used, ",", "", -1), 10, 64)
	tweetCap.Total, _ = strconv.ParseInt(strings.Replace(total, ",", "", -1), 10, 64)
	tweetCap.Remaining = tweetCap.Total - tweetCap.Used
	tweetCap.Resets, _ = time.Parse("January 2 15:04 UTC", fmt.Sprintf("%s %d %s %s", month, day, t, tz))
	tweetCap.Resets = time.Date(
		time.Now().UTC().Year(),
		tweetCap.Resets.Month(),
		tweetCap.Resets.Day(),
		tweetCap.Resets.Hour(),
		tweetCap.Resets.Minute(),
		0, 0,
		tweetCap.Resets.Location(),
	)
	w.TweetCap = tweetCap
	return nil
}

// CheckRateLimit will return an appropriate error if the given number of tweets exceeds either the tweet cap or the
// given RequestRateLimit.
func (w *ClientWrapper) CheckRateLimit(rateLimit RequestRateLimit, totalTweets int64) (err error) {
	w.Mutex.Lock()
	// If we don't have enough of our monthly tweet cap remaining to fetch the requested number of tweets then we will
	// return an error.
	if totalTweets > w.TweetCap.Remaining {
		return TemporaryErrorf(
			false,
			"cannot request %d tweets as our tweet cap is %s",
			totalTweets,
			w.TweetCap.String(),
		)
	}
	w.Mutex.Unlock()
	// If we wanted to fetch the entire days tweets (55000) but we can only make 450 requests every 15 minutes each
	// requesting 100 tweets then we will also return an error as this request will take up to 15 minutes to process.
	if int(totalTweets) > rateLimit.Requests*MaxTweetsPerRequest {
		return TemporaryErrorf(
			false,
			"cannot request %d tweets as the maximum for this action is %s (%d tweets)",
			totalTweets,
			rateLimit.String(),
			rateLimit.Requests*MaxTweetsPerRequest,
		)
	}
	return nil
}

// BindingType represents a binding for the ClientWrapper.
type BindingType int

const (
	// RecentSearch binds to the twitter.Client.TweetRecentSearch method.
	RecentSearch BindingType = iota
)

// WrapResponse wraps the given response from twitter.Client in a prototype that implements the BindingResult interface.
func (bt BindingType) WrapResponse(response any) (result BindingResult, err error) {
	bindingResult := &struct{ bindingResultProto }{}
	switch bt {
	case RecentSearch:
		tweetRecentSearchResponse := response.(*twitter.TweetRecentSearchResponse)
		bindingResult.rawMethod = func() *twitter.TweetRaw { return tweetRecentSearchResponse.Raw }
		bindingResult.metaMethod = func() BindingResultMeta {
			resultMeta := &struct{ bindingResultMetaProto }{}
			resultMeta.newestIDMethod = func() string { return tweetRecentSearchResponse.Meta.NewestID }
			resultMeta.oldestIDMethod = func() string { return tweetRecentSearchResponse.Meta.OldestID }
			resultMeta.resultCountMethod = func() int { return tweetRecentSearchResponse.Meta.ResultCount }
			resultMeta.nextTokenMethod = func() string { return tweetRecentSearchResponse.Meta.NextToken }
			return resultMeta
		}
	default:
		return nil, fmt.Errorf("cannot find RecentSearch %d", bt)
	}
	return bindingResult, nil
}

// RequestRateLimit represents a request rate limit for a Binding.
type RequestRateLimit struct {
	Requests int
	Every    time.Duration
}

// String returns a string representation of the RequestRateLimit.
func (rrl *RequestRateLimit) String() string {
	return fmt.Sprintf("%d requests every %s", rrl.Requests, rrl.Every.String())
}

// Binding represents a binding for the ClientWrapper and wraps a Request function, the RequestRateLimit for the action,
// and the BindingType of the Binding.
type Binding struct {
	Request          func(client *ClientWrapper, options *BindingOptions, args ...any) (any, error)
	RequestRateLimit RequestRateLimit
	BindingType      BindingType
}

// BindingResultMeta is an interface for the Meta field that can be found in many of the responses from twitter.Client.
type BindingResultMeta interface {
	NewestID() string
	OldestID() string
	ResultCount() int
	NextToken() string
}

// bindingResultMetaProto is the prototype for BindingResultMeta that implements BindingResultMeta.
type bindingResultMetaProto struct {
	newestIDMethod    func() string
	oldestIDMethod    func() string
	resultCountMethod func() int
	nextTokenMethod   func() string
}

func (brmp *bindingResultMetaProto) NewestID() string  { return brmp.newestIDMethod() }
func (brmp *bindingResultMetaProto) OldestID() string  { return brmp.oldestIDMethod() }
func (brmp *bindingResultMetaProto) ResultCount() int  { return brmp.resultCountMethod() }
func (brmp *bindingResultMetaProto) NextToken() string { return brmp.nextTokenMethod() }

// BindingResult is an interface for the fields that can be found in many of the responses from the twitter.Client.
type BindingResult interface {
	Raw() *twitter.TweetRaw
	Meta() BindingResultMeta
	RateLimit() *twitter.RateLimit
}

// bindingResultProto is the prototype for BindingResult that implements BindingResult.
type bindingResultProto struct {
	rawMethod       func() *twitter.TweetRaw
	metaMethod      func() BindingResultMeta
	rateLimitMethod func() *twitter.RateLimit
}

func (brp *bindingResultProto) Raw() *twitter.TweetRaw        { return brp.rawMethod() }
func (brp *bindingResultProto) Meta() BindingResultMeta       { return brp.metaMethod() }
func (brp *bindingResultProto) RateLimit() *twitter.RateLimit { return brp.rateLimitMethod() }

// BindingOptions are the options available for the execution of a Binding.
type BindingOptions struct {
	Total int64
}

// clientBindings contains a mapping of BindingType to Binding. This contains all the bindings that are possible to
// execute.
var clientBindings = map[BindingType]Binding{
	RecentSearch: {
		Request: func(client *ClientWrapper, options *BindingOptions, args ...any) (response any, err error) {
			if response, err = client.Client.TweetRecentSearch(
				context.Background(),
				args[0].(string),
				args[1].(twitter.TweetRecentSearchOpts),
			); err != nil {
				return response, err
			}
			return response, nil
		},
		RequestRateLimit: RequestRateLimit{Requests: 450, Every: time.Minute * 15},
		BindingType:      RecentSearch,
	},
}

// ExecuteBinding executes the given BindingType with the given BindingOptions and returns a prototype that implements
// the BindingResult. It will check if the rate limit has been/will be exceeded using the ClientWrapper.CheckRateLimit
// method before executing the Binding.Request.
func (w *ClientWrapper) ExecuteBinding(bindingType BindingType, options *BindingOptions, args ...any) (bindingResult BindingResult, err error) {
	binding := clientBindings[bindingType]
	if options != nil {
		if err = w.CheckRateLimit(binding.RequestRateLimit, options.Total); err != nil {
			return bindingResult, errors.Wrap(err, "could not")
		}
	}
	var response any
	if response, err = binding.Request(w, options, args...); err != nil {
		return bindingResult, errors.Wrap(err, "")
	}
	return binding.BindingType.WrapResponse(response)
}
