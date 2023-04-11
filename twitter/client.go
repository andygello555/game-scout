package twitter

import (
	"encoding/json"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	myErrors "github.com/andygello555/agem"
	"github.com/andygello555/game-scout/browser"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ClientWrapper struct {
	Client     *twitter.Client
	Config     Config
	Mutex      sync.Mutex
	RateLimits map[BindingType]*twitter.RateLimit
	TweetCap   *TweetCap
}

var Client *ClientWrapper

type authorize struct {
	Token string
}

func (a authorize) Add(req *http.Request) {
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.Token))
}

type RateLimits interface {
	LimitPerMonth() uint64
	LimitPerWeek() uint64
	LimitPerDay() uint64
	LimitPerHour() uint64
	LimitPerMinute() uint64
	LimitPerSecond() uint64
	LimitPerRequest() time.Duration
}

type Config interface {
	TwitterAPIKey() string
	TwitterAPIKeySecret() string
	TwitterBearerToken() string
	TwitterUsername() string
	TwitterPassword() string
	TwitterHashtags() []string
	TwitterQuery() string
	TwitterHeadless() bool
	TwitterRateLimits() RateLimits
	TwitterTweetCapLocation() string
	TwitterCreatedAtFormat() string
	TwitterIgnoredErrorTypes() []string
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
		Config:     config,
		RateLimits: make(map[BindingType]*twitter.RateLimit),
	}
	if err := Client.GetTweetCap(); err != nil {
		return errors.Wrap(err, "could not create client, could not get Tweet cap")
	}
	return nil
}

// TweetCap represents the overall Tweet cap for a Twitter API app.
type TweetCap struct {
	Used        int
	Remaining   int
	Total       int
	Resets      time.Time
	LastFetched time.Time
}

// String returns the string representation of the Tweet cap in the format:
// "<Used>/<Total> <Used/Total*100>% (<Remaining> rem.) resets: <Resets>, fetched: <LastFetched>".
func (tc *TweetCap) String() string {
	return fmt.Sprintf(
		"%d/%d %.2f%% (%d rem.) resets: %s, fetched: %s",
		tc.Used, tc.Total, (float64(tc.Used)/float64(tc.Total))*100.0, tc.Remaining,
		tc.Resets.String(), tc.LastFetched.String(),
	)
}

// CheckFresh will check if the TweetCap needs to be refreshed via the web (ClientWrapper.GetTweetCap).
func (tc *TweetCap) CheckFresh() bool {
	// If the TweetCap was last fetched before the reset date and the time now is after the reset date then we need to
	// fetch new info from the web
	return !(tc.LastFetched.Before(tc.Resets) && time.Now().UTC().After(tc.Resets))
}

// GetTweetCap will first check if the TweetCap cache file exists. If so, then it will read the TweetCap from there.
// Otherwise, it will log in to the Twitter developer portal using browser.Browser and parse the details on the
// dashboard to a TweetCap instance.
func (w *ClientWrapper) GetTweetCap() (err error) {
	log.INFO.Println("Getting TweetCap")
	// We first check if the TweetCap file exists. If it does, we read in the TweetCap from there.
	if _, err = os.Stat(w.Config.TwitterTweetCapLocation()); !errors.Is(err, os.ErrNotExist) {
		log.INFO.Printf("\tTweetCap cache file exists, reading it in from %s", w.Config.TwitterTweetCapLocation())
		var tweetCapData []byte
		if tweetCapData, err = os.ReadFile(w.Config.TwitterTweetCapLocation()); err != nil {
			return err
		}

		w.TweetCap = &TweetCap{}
		if err = json.Unmarshal(tweetCapData, w.TweetCap); err != nil {
			return err
		}
	}

	// If the TweetCap was not loaded from a file, or the TweetCap is not fresh then we will fetch the TweetCap using a
	// browser.Browser.
	if w.TweetCap == nil || !w.TweetCap.CheckFresh() {
		message := "not fresh"
		if w.TweetCap == nil {
			message = "nil"
		} else {
			message += " (" + w.TweetCap.String() + ")"
		}
		log.INFO.Printf("\tTweetCap is %s, fetching TweetCap from web", message)
		var b *browser.Browser
		if b, err = browser.NewBrowser(w.Config.TwitterHeadless()); err != nil {
			return errors.Wrap(err, "could not create browser to get TweetCap")
		}
		var outputs []string
		if outputs, err = b.Flow(0,
			// Goto the dashboard page. This should prompt a login attempt
			browser.Command{
				Type:  browser.Goto,
				Value: "https://developer.twitter.com/en/portal/dashboard",
			},
			// Select the username input field
			browser.Command{
				Type:  browser.Select,
				Value: "input[autocomplete=username]",
				Wait:  true,
			},
			// Type the Twitter username into the input field
			browser.Command{
				Type:  browser.Type,
				Value: w.Config.TwitterUsername(),
			},
			// Select all the buttons on the page
			browser.Command{
				Type:  browser.SelectAll,
				Value: "div[role=button]",
			},
			// Find the buttons that contain the text: "Continue", so that we can move onto the next part of the form
			browser.Command{
				Type: browser.FindInnerText,
				FindFunc: func(element any) bool {
					return strings.Contains(element.(string), "Next")
				},
			},
			// Click the Continue button
			browser.Command{
				Type: browser.Click,
			},
			// If the login flow prompts us to input our username again we will
			browser.Command{
				Type:     browser.Select,
				Value:    "input[data-testid=ocfEnterTextTextInput]",
				Optional: true,
				Wait:     true,
			},
			// Type the username into the prompt. This is the first part of the email address usually
			browser.Command{
				Type:     browser.Type,
				Value:    strings.Split(w.Config.TwitterUsername(), "@")[0],
				Optional: true,
			},
			// Find the next button
			browser.Command{
				Type:     browser.Select,
				Value:    "div[data-testid=ocfEnterTextNextButton]",
				Wait:     true,
				Optional: true,
			},
			// Click the next button
			browser.Command{
				Type:     browser.Click,
				Optional: true,
			},
			// Find the field for the password input
			browser.Command{
				Type:  browser.Select,
				Value: "input[autocomplete=current-password]",
				Wait:  true,
			},
			// Enter the password
			browser.Command{
				Type:  browser.Type,
				Value: w.Config.TwitterPassword(),
			},
			// Find all buttons on the page
			browser.Command{
				Type:  browser.SelectAll,
				Value: "div[role=button]",
			},
			// Find the button that contains the text: "Log in", to continue the login flow
			browser.Command{
				Type: browser.FindInnerText,
				FindFunc: func(element any) bool {
					return strings.Contains(element.(string), "Log in")
				},
			},
			// Click the "Log in" button
			browser.Command{
				Type: browser.Click,
			},
			// We wait until an element on the dashboard has appeared that contains the TweetCap
			browser.Command{
				Type:  browser.Select,
				Value: "div[class*=index__messageContainer]",
				Wait:  true,
			},
			// We extract the text from the element containing the TweetCap, so we can parse it later on
			browser.Command{
				Type: browser.InnerText,
			},
		); err != nil {
			return errors.Wrap(err, "could not execute Flow successfully to get TweetCap")
		}
		if err = b.Quit(); err != nil {
			return errors.Wrap(err, "could not quit Browser successfully after getting TweetCap")
		}

		tweetCapString := outputs[0]
		var used, total, month, t, tz string
		var day int
		if _, err = fmt.Sscanf(tweetCapString, "%s Tweets pulled of %s\nResets on %s %d at %s %s", &used, &total, &month, &day, &t, &tz); err != nil {
			return errors.Wrapf(err, "could not parse Tweet Cap string \"%s\"", tweetCapString)
		}
		tweetCap := &TweetCap{}
		var usedInt, totalInt int64
		usedInt, _ = strconv.ParseInt(strings.Replace(used, ",", "", -1), 10, 64)
		totalInt, _ = strconv.ParseInt(strings.Replace(total, ",", "", -1), 10, 64)
		tweetCap.Used = int(usedInt)
		tweetCap.Total = int(totalInt)
		tweetCap.Remaining = tweetCap.Total - tweetCap.Used
		tweetCap.Resets, _ = time.Parse("January 2 15:04 UTC", fmt.Sprintf("%s %d %s %s", month, day, t, tz))

		// We have to check if the date referenced by the Twitter dashboard is for next year. This is because they don't
		// give us a year on the dashboard.
		todayYear, todayMonth, _ := time.Now().UTC().Date()
		if todayMonth == time.December && tweetCap.Resets.Month() < time.December {
			todayYear += 1
		}

		// Then we can construct the proper time.Time with the correct year
		tweetCap.Resets = time.Date(
			todayYear,
			tweetCap.Resets.Month(),
			tweetCap.Resets.Day(),
			tweetCap.Resets.Hour(),
			tweetCap.Resets.Minute(),
			0, 0,
			tweetCap.Resets.Location(),
		)
		tweetCap.LastFetched = time.Now().UTC()
		w.TweetCap = tweetCap
		log.INFO.Printf("\t\tSuccessfully fetched new TweetCap from web: %s", w.TweetCap.String())
		if err = w.WriteTweetCap(); err != nil {
			return errors.Wrap(err, "could not write TweetCap cache file")
		}
		log.INFO.Printf("\tSuccessfully wrote TweetCap to %s", w.Config.TwitterTweetCapLocation())
	}
	log.INFO.Printf("Successfully fetched TweetCap: %s", w.TweetCap.String())
	return nil
}

// WriteTweetCap will write the TweetCap to a JSON cache file located at DefaultTweetCapLocation.
func (w *ClientWrapper) WriteTweetCap() (err error) {
	var jsonBytes []byte
	if jsonBytes, err = json.Marshal(w.TweetCap); err != nil {
		return errors.Wrap(err, "could not Marshal TweetCap to JSON")
	}
	var file *os.File
	if file, err = os.Create(w.Config.TwitterTweetCapLocation()); err != nil {
		return errors.Wrapf(err, "could not create TweetCap file %s", w.Config.TwitterTweetCapLocation())
	}
	defer func(file *os.File) {
		err = myErrors.MergeErrors(err, errors.Wrapf(
			file.Close(),
			"could not close TweetCap file %s",
			w.Config.TwitterTweetCapLocation(),
		))
	}(file)
	if _, err = file.Write(jsonBytes); err != nil {
		return errors.Wrapf(err, "could not write to TweetCap file %s", w.Config.TwitterTweetCapLocation())
	}
	return nil
}

// SetTweetCap will set the TweetCap field of the given ClientWrapper but also save the TweetCap to a cache JSON file to
// be read later on.
func (w *ClientWrapper) SetTweetCap(used int, remaining int, total int, resets time.Time, lastFetched time.Time) (err error) {
	log.INFO.Printf(
		"Setting TweetCap to: %d used, %d remaining, %d total, %s reset, %s fetched",
		used, remaining, total, resets.String(), lastFetched.String(),
	)
	w.Mutex.Lock()
	defer w.Mutex.Unlock()
	if w.TweetCap == nil {
		w.TweetCap = &TweetCap{}
	}
	w.TweetCap.Used = used
	w.TweetCap.Remaining = remaining
	w.TweetCap.Total = total
	w.TweetCap.Resets = resets
	w.TweetCap.LastFetched = lastFetched

	if !w.TweetCap.CheckFresh() {
		log.WARNING.Printf("TweetCap (%s) is not fresh, fetching from web...", w.TweetCap.String())
		if err = w.GetTweetCap(); err != nil {
			return errors.Wrap(
				err,
				fmt.Sprintf(
					"could not get the TweetCap after discovering it was last fetched before the reset time %s < %s",
					lastFetched.String(),
					resets.String(),
				),
			)
		}
	} else {
		// Otherwise, we just write the TweetCap to the cache file
		log.INFO.Printf(
			"TweetCap (%s) is fresh, writing to cache: %s",
			w.TweetCap.String(), w.Config.TwitterTweetCapLocation(),
		)
		if err = w.WriteTweetCap(); err != nil {
			return errors.Wrap(err, "could not write TweetCap cache file")
		}
	}
	return nil
}

// CheckRateLimit will return an appropriate error if the given number of resources exceeds either the tweet cap or the
// given RequestRateLimit.
func (w *ClientWrapper) CheckRateLimit(binding *Binding, totalResources int) (err error) {
	now := time.Now().UTC()

	// We create copies of both the tweet cap and the current rate limit for this action (if there is one) so we don't
	// have to lock the mutex for a long time.
	w.Mutex.Lock()
	tweetCap := TweetCap{
		Used:        w.TweetCap.Used,
		Remaining:   w.TweetCap.Remaining,
		Total:       w.TweetCap.Total,
		Resets:      w.TweetCap.Resets,
		LastFetched: w.TweetCap.LastFetched,
	}
	rateLimit, rateLimitOk := w.RateLimits[binding.Type]
	if rateLimitOk {
		rateLimit = &twitter.RateLimit{
			Limit:     rateLimit.Limit,
			Remaining: rateLimit.Remaining,
			Reset:     rateLimit.Reset,
		}
	}
	w.Mutex.Unlock()

	// If we don't have enough of our monthly tweet cap remaining to fetch the requested number of tweets then we will
	// return an error.
	if binding.ResourceType == Tweet && totalResources > tweetCap.Remaining {
		log.WARNING.Printf("TweetCap check for %s failed when requesting %d tweets (%s)", binding.Type.String(), totalResources, w.TweetCap.String())
		return &RateLimitError{
			Config:                 w.Config.TwitterRateLimits(),
			TweetCap:               &tweetCap,
			MaxResourcesPerRequest: binding.MaxResourcesPerRequest,
			RequestedResources:     totalResources,
			RequestRateLimit:       &binding.RequestRateLimit,
			ResourceType:           binding.ResourceType,
			Happened:               now,
		}
	}
	// Then we check if there is a saved twitter.RateLimit for this BindingType in the ClientWrapper. If there is, we
	// can check if we can make enough requests with the remaining rate limit. We only need to check the rate limit if
	// the reset time is after the current time, this is because we don't really care if the rate limit is stale and
	// assume that the rate has reset.
	if rateLimitOk && rateLimit.Reset.Time().After(now) {
		log.INFO.Printf("We have a saved RateLimit for %s which is still valid for now", binding.Type.String())
		if totalResources > rateLimit.Remaining*binding.MaxResourcesPerRequest {
			log.WARNING.Printf(
				"%d %ss exceeds rate limit of %d resources per request until %s",
				totalResources, binding.ResourceType.String(), binding.MaxResourcesPerRequest,
				rateLimit.Reset.Time().String(),
			)
			return &RateLimitError{
				Config:                 w.Config.TwitterRateLimits(),
				RateLimit:              rateLimit,
				MaxResourcesPerRequest: binding.MaxResourcesPerRequest,
				RequestedResources:     totalResources,
				ResourceType:           binding.ResourceType,
				Happened:               now,
			}
		}

		// If the number of requests we can make in the remaining time on the rate limit exceeds the number of requests
		// we would have to make to satisfy the totalResources to get. We also check if remaining is != 0. This is
		// because the rate limit will just about be reset by the time we make the actual request.
		if remaining := rateLimit.Reset.Time().Sub(now) / w.Config.TwitterRateLimits().LimitPerRequest(); int64(remaining) < int64(totalResources/binding.MaxResourcesPerRequest) && remaining != 0 {
			log.WARNING.Printf(
				"There is %s remaining on the rate limit meaning we cannot make the %d requests required for %d %ss",
				remaining.String(),
				totalResources/binding.MaxResourcesPerRequest,
				totalResources,
				binding.ResourceType.String(),
			)
			return &RateLimitError{
				Config:                 w.Config.TwitterRateLimits(),
				RateLimit:              rateLimit,
				MaxResourcesPerRequest: binding.MaxResourcesPerRequest,
				RequestedResources:     totalResources,
				ResourceType:           binding.ResourceType,
				Happened:               now,
			}
		} else if remaining == 0 {
			// Wait however long it takes for the rateLimit to reset. This will never be longer than LimitPerRequest.
			waitTime := rateLimit.Reset.Time().Sub(now)
			log.WARNING.Printf(
				"(%s - %s) / %s == 0, so we will wait %s until the rateLimit reset time",
				rateLimit.Reset.Time().String(), now.String(), w.Config.TwitterRateLimits().LimitPerRequest(), waitTime,
			)
			time.Sleep(waitTime)
		}
	}

	// If the total number of resources we are requesting exceeds the maximum number of resources we can fetch with the
	// Binding's rate limit then we will return an appropriate error.
	if totalResources > binding.RequestRateLimit.Requests*binding.MaxResourcesPerRequest {
		log.WARNING.Printf(
			"%d %ss exceeds the number of %ss we could possibly ever request from %s (max: %d)",
			totalResources, binding.ResourceType.String(), binding.ResourceType.String(),
			binding.Type.String(), binding.RequestRateLimit.Requests*binding.MaxResourcesPerRequest,
		)
		return &RateLimitError{
			Config: w.Config.TwitterRateLimits(),
			RequestRateLimit: &RequestRateLimit{
				Requests: binding.RequestRateLimit.Requests,
				Every:    binding.RequestRateLimit.Every,
			},
			MaxResourcesPerRequest: binding.MaxResourcesPerRequest,
			RequestedResources:     totalResources,
			ResourceType:           binding.ResourceType,
			Happened:               now,
		}
	}
	return nil
}

// ExecuteBinding executes the given BindingType with the given BindingOptions and returns a prototype that implements
// the BindingResult. It will check if the rate limit has been/will be exceeded using the ClientWrapper.CheckRateLimit
// method before executing the Binding.Request.
func (w *ClientWrapper) ExecuteBinding(bindingType BindingType, options *BindingOptions, args ...any) (bindingResult BindingResult, err error) {
	// We only update it if it is newer than the previous rate limit for this binding type, or there is no saved
	// rate limit for this binding type yet. We can check if the rate limit is newer by looking at the reset times
	// and the number of requests remaining.
	updateRateLimits := func() {
		rateLimit := bindingResult.RateLimit()
		w.Mutex.Lock()
		latestRateLimit, ok := w.RateLimits[bindingType]
		update := !ok
		if ok {
			// We initialise some booleans to make things more readable
			samePeriod := rateLimit.Reset.Time().Equal(latestRateLimit.Reset.Time())
			smallerRemaining := rateLimit.Remaining < latestRateLimit.Remaining
			afterPeriod := rateLimit.Reset.Time().After(latestRateLimit.Reset.Time())
			update = (samePeriod && smallerRemaining) || afterPeriod
		}
		if update {
			log.INFO.Printf(
				"Updating RateLimit for %s to %d/%d (%s)",
				bindingType.String(), rateLimit.Remaining, rateLimit.Limit, rateLimit.Reset.Time().String(),
			)
			w.RateLimits[bindingType] = rateLimit
		} else {
			log.WARNING.Printf(
				"RateLimit for %s: %d/%d (%s) was not newer than the currently cached RateLimit for %s: %d/%d (%s)",
				bindingType.String(), rateLimit.Remaining, rateLimit.Limit, rateLimit.Reset.Time().String(),
				bindingType.String(), latestRateLimit.Remaining, latestRateLimit.Limit, latestRateLimit.Reset.Time().String(),
			)
		}
		w.Mutex.Unlock()
	}

	var response any
	binding := clientBindings[bindingType]
	start := time.Now().UTC()
	// If there are functions and values that indicate that the binding can be batched then we will initiate a loop for
	// batch requests.
	if options != nil &&
		binding.SetOptionsForCurrentRequest != nil &&
		binding.SetOptionsForNextRequest != nil &&
		options.Total > binding.MaxResourcesPerRequest {
		log.INFO.Printf("ExecuteBinding is using a batching process for %s(%v)", bindingType.String(), options)
		requestNo := 1
		for i := 0; i < options.Total; i += binding.MaxResourcesPerRequest {
			// First we find out the number of resources to request from this binding.
			offset := binding.MaxResourcesPerRequest
			if i+offset > options.Total {
				offset = options.Total - i
			}
			// If the offset is less than the minimum number of possible requests for this Binding then we will bump it
			// up to the minimum
			if offset < binding.MinResourcesPerRequest {
				log.INFO.Printf("Bumping up offset from %d to %d, the minimum for %s", offset, binding.MinResourcesPerRequest, binding.Type.String())
				offset = binding.MinResourcesPerRequest
			}

			// Then we check the rate limit to make sure we can actually make this request.
			if err = w.CheckRateLimit(&binding, offset); err != nil {
				return bindingResult, errors.Wrapf(err, "rate limit check failed for request no. %d", requestNo)
			}

			// Set the options for the current request. This usually just updates the resources returned by this
			// request.
			if args, err = binding.SetOptionsForCurrentRequest(offset, args...); err != nil {
				return bindingResult, errors.Wrapf(err, "could not set options for current request no. %d", requestNo)
			}

			log.INFO.Printf("\tExecuteBinding is on batch %d with offset: %d", requestNo, offset)

			// Then we make the request itself, passing in the client, options and args.
			if response, err = binding.Request(w, options, args...); err != nil {
				var errorResponse *twitter.ErrorResponse
				if errors.As(err, &errorResponse) {
					log.ERROR.Printf(
						"Could not make %s request no. %d for the following reasons: %s (%v)",
						binding.Type.String(), requestNo, err.Error(), err.(*twitter.ErrorResponse).Errors,
					)
				}
				return bindingResult, errors.Wrapf(err, "could not make %s request no. %d", binding.Type.String(), requestNo)
			}

			// Then we wrap the response in a BindingResult instance.
			var nextBindingResult BindingResult
			if nextBindingResult, err = binding.Type.WrapResponse(w, response); err != nil {
				return bindingResult, errors.Wrapf(err, "could not wrap response from request no. %d", requestNo)
			}

			// If the accumulator bindingResult has not yet been set, because it is the initial request, then we will
			// set it to be the newly wrapped result.
			if bindingResult == nil {
				bindingResult = nextBindingResult
			} else {
				// Otherwise, if it's not the initial request, then we will merge the newly wrapped result with the
				// accumulated response: bindingResult.
				if err = bindingResult.MergeNext(nextBindingResult, &binding); err != nil {
					return bindingResult, errors.Wrapf(err, "could not merge response from request no. %d and %d", requestNo-1, requestNo)
				}
			}

			// We then set the next request's options using the Binding.SetOptionsForNextRequest method. This returns a
			// new list of arguments to pass to the Binding.Request method.
			requestNo++
			if args, err = binding.SetOptionsForNextRequest(bindingResult, args...); err != nil {
				return bindingResult, errors.Wrapf(err, "could not set options for next request no. %d", requestNo)
			}

			// Finally, we update the rate limits. First up it's the Tweet cap (if the BindingResourceType is Tweet)
			if binding.ResourceType == Tweet {
				if err = w.SetTweetCap(w.TweetCap.Used+offset, w.TweetCap.Remaining-offset, w.TweetCap.Total, w.TweetCap.Resets, w.TweetCap.LastFetched); err != nil {
					return bindingResult, errors.Wrapf(err, "could not set TweetCap after request no. %d", requestNo-1)
				}
			}

			updateRateLimits()
		}
		log.INFO.Printf("ExecuteBinding has completed batching process for %d requests in %s", requestNo-1, time.Now().UTC().Sub(start).String())
	} else {
		log.INFO.Printf("ExecuteBinding using a singleton request for %s(%v)", bindingType.String(), options)
		// When BindingOptions is not given or total is less than the maximum number of resources possible to request
		// from this Binding, we will check the MaxResults expected to be returned by the binding against the RateLimit,
		// then execute a singleton request.
		maxResults := binding.MaxResourcesPerRequest
		if binding.ClampMaxResults != nil {
			maxResults, args = binding.ClampMaxResults(&binding, args...)
		}
		if err = w.CheckRateLimit(&binding, maxResults); err != nil {
			return bindingResult, errors.Wrapf(err, "rate limit check failed for singleton %s request", binding.Type.String())
		}

		// Then we make the Request itself.
		if response, err = binding.Request(w, options, args...); err != nil {
			var errorResponse *twitter.ErrorResponse
			if errors.As(err, &errorResponse) {
				log.ERROR.Printf(
					"Could not make singleton %s request for the following reasons: %s (%v)",
					binding.Type.String(), err.Error(), err.(*twitter.ErrorResponse).Errors,
				)
			}
			return bindingResult, errors.Wrapf(err, "could not make singleton %s request", binding.Type.String())
		}

		// And then we wrap the response in a BindingResult.
		if bindingResult, err = binding.Type.WrapResponse(w, response); err != nil {
			return bindingResult, errors.Wrapf(err, "could not wrap response from singleton %s request", binding.Type.String())
		}

		// Finally, we update the rate limits. First up it's the Tweet cap (if the BindingResourceType is Tweet)
		if binding.ResourceType == Tweet {
			if err = w.SetTweetCap(w.TweetCap.Used+maxResults, w.TweetCap.Remaining-maxResults, w.TweetCap.Total, w.TweetCap.Resets, w.TweetCap.LastFetched); err != nil {
				return bindingResult, errors.Wrapf(err, "could not set TweetCap after singleton %s request", binding.Type.String())
			}
		}

		updateRateLimits()
		log.INFO.Printf("ExecuteBinding has completed the singleton request in %s", time.Now().UTC().Sub(start).String())
	}
	return bindingResult, nil
}
