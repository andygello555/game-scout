package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	myErrors "github.com/andygello555/agem"
	"github.com/andygello555/gapi"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var DefaultClient *Client

type accessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

type AccessToken struct {
	accessTokenResponse
	FetchedTime time.Time
	ExpireTime  time.Time
}

func (at AccessToken) Expired() bool {
	return time.Now().UTC().After(at.ExpireTime)
}

func (at AccessToken) Headers() http.Header {
	return http.Header{
		"Authorization": []string{fmt.Sprintf("Bearer: %s", at.AccessToken)},
	}
}

type RateLimit struct {
	used      int64
	remaining int64
	reset     time.Time
}

// RateLimitFromHeader returns a new RateLimit instance from the given http.Header by fetching and parsing the values
// for the following headers:
//   - X-Ratelimit-Remaining
//   - X-Ratelimit-Reset
//   - X-Ratelimit-Used
//
// If not all headers are found, nil will be returned.
func RateLimitFromHeader(header http.Header) (rl *RateLimit, err error) {
	_, ok1 := header["X-Ratelimit-Remaining"]
	_, ok2 := header["X-Ratelimit-Reset"]
	_, ok3 := header["X-Ratelimit-Used"]
	if !ok1 || !ok2 || !ok3 {
		return nil, nil
	}

	rl = &RateLimit{}
	if rl.remaining, err = strconv.ParseInt(header.Get("X-Ratelimit-Remaining"), 10, 64); err != nil {
		err = errors.Wrap(err, "cannot parse \"X-Ratelimit-Remaining\" header to int")
		return
	}

	if rl.used, err = strconv.ParseInt(header.Get("X-Ratelimit-Used"), 10, 64); err != nil {
		err = errors.Wrap(err, "cannot parse \"X-Ratelimit-Used\" header to int")
		return
	}

	var resetSeconds int64
	if resetSeconds, err = strconv.ParseInt(header.Get("X-Ratelimit-Reset"), 10, 64); err != nil {
		err = errors.Wrap(err, "cannot \"X-Ratelimit-Reset\" header to int")
		return
	}
	rl.reset = time.Now().UTC().Add(time.Second * time.Duration(resetSeconds))
	return
}

func (r *RateLimit) Reset() time.Time        { return r.reset.Truncate(time.Second) }
func (r *RateLimit) Remaining() int          { return int(r.remaining) }
func (r *RateLimit) Used() int               { return int(r.used) }
func (r *RateLimit) Type() api.RateLimitType { return api.RequestRateLimit }

func (r *RateLimit) String() string {
	total := r.Used() + r.Remaining()
	return fmt.Sprintf(
		"%d/%d used (%.2f%%) resets %s",
		r.Used(), total, (float64(r.Used())/float64(total))*100.0, r.Reset().String(),
	)
}

// requestsPeriodCountMax is the maximum number of periods to store in the Client.requestsPeriodCount sync.Map. This is
// just above 24 hours of periods.
const requestsPeriodCountMax = 300

type Client struct {
	Config           Config
	accessTokenMutex sync.RWMutex
	accessToken      *AccessToken
	// rateLimits is a sync.Map of binding names to references to api.RateLimit(s). I.e.
	//  map[string]api.RateLimit
	rateLimits sync.Map
	// requestsPeriodCount is a sync.Map of timestamps at 5 minute intervals to a counter for how many requests were
	// made in that interval. sync/atomic is used to atomically increment the counter integers. The type of the map is
	// as follows:
	//  map[int64]*atomic.Uint32
	// The map never contains more than requestsPeriodCountMax periods.
	requestsPeriodCount sync.Map
	// requestPeriods is the number of periods currently within requestsPeriodCount.
	requestPeriods *atomic.Uint32
}

func (c *Client) RateLimits() *sync.Map { return &c.rateLimits }

func (c *Client) AddRateLimit(bindingName string, rateLimit api.RateLimit) {
	if rlAny, ok := c.rateLimits.Load(bindingName); ok {
		// If there is already a RateLimit for this binding, check if the rate-limit returned by the current request is
		// newer.
		rl := rlAny.(api.RateLimit)
		gte := rateLimit.Reset().After(rl.Reset()) || rateLimit.Reset().Equal(rl.Reset())
		usedMore := rateLimit.Used() > rl.Used()
		if gte && usedMore {
			c.rateLimits.Store(bindingName, rateLimit)
		}
	} else {
		c.rateLimits.Store(bindingName, rateLimit)
	}
}

func (c *Client) LatestRateLimit(bindingName string) api.RateLimit {
	var latestRateLimit api.RateLimit
	totalRateLimits := 0
	c.rateLimits.Range(func(key, value any) bool {
		rl := value.(api.RateLimit)
		if latestRateLimit == nil {
			latestRateLimit = rl
		} else {
			gte := latestRateLimit.Reset().After(rl.Reset()) || latestRateLimit.Reset().Equal(rl.Reset())
			usedMore := rl.Used() > latestRateLimit.Used()
			if gte && usedMore {
				latestRateLimit = rl
			}
		}
		totalRateLimits++
		return true
	})
	return latestRateLimit
}

func (c *Client) Log(msg string) {
	log.WARNING.Println(msg)
}

func (c *Client) AccessToken() *AccessToken {
	c.accessTokenMutex.RLock()
	defer c.accessTokenMutex.RUnlock()
	if c.accessToken == nil {
		return nil
	}
	return &AccessToken{
		accessTokenResponse: c.accessToken.accessTokenResponse,
		FetchedTime:         c.accessToken.FetchedTime,
		ExpireTime:          c.accessToken.ExpireTime,
	}
}

func (c *Client) setAccessToken(response accessTokenResponse) {
	c.accessTokenMutex.Lock()
	defer c.accessTokenMutex.Unlock()
	c.accessToken = &AccessToken{
		accessTokenResponse: response,
		FetchedTime:         time.Now().UTC(),
		ExpireTime:          time.Now().UTC().Add(time.Second * time.Duration(response.ExpiresIn)),
	}
}

func (c *Client) RefreshToken() (err error) {
	refresh := func() bool {
		c.accessTokenMutex.RLock()
		defer c.accessTokenMutex.RUnlock()
		return c.accessToken == nil || c.accessToken.Expired()
	}()

	if refresh {
		if _, err = API.Execute("access_token"); err != nil {
			err = errors.Wrap(err, "could not fetched access_token for Reddit API")
		}
	}
	return
}

// incrementRequestPeriod increments the current request period (5 minute periods). If there is a new period we will
// delete the oldest period so that we keep the map at
func (c *Client) incrementRequestPeriod() {
	var count atomic.Uint32
	period := time.Now().UTC().Truncate(time.Minute * 5).Unix()
	countAny, loaded := c.requestsPeriodCount.LoadOrStore(period, &count)
	countAny.(*atomic.Uint32).Add(1)

	// Only if we have just stored a new key we will check if the length of the map exceeds the requestsPeriodCountMax.
	if !loaded {
		// If it does then we will find the minimum time period and delete it.
		if c.requestPeriods.Load() == requestsPeriodCountMax {
			var minTime *int64
			c.requestsPeriodCount.Range(func(key, value any) bool {
				t := key.(int64)
				if minTime == nil {
					minTime = &t
				} else if t < *minTime {
					minTime = &t
				}
				return true
			})
			// Delete the minimum time period
			c.requestsPeriodCount.Delete(*minTime)
		} else {
			// Otherwise, we will increment the requestPeriods
			c.requestPeriods.Add(1)
		}
	}
}

// RequestsMadeWithinPeriod returns the number of Reddit API requests made within the given time period.
func (c *Client) RequestsMadeWithinPeriod(period time.Duration) uint32 {
	var count uint32
	today := time.Now().UTC().Truncate(period)
	c.requestsPeriodCount.Range(func(key, value any) bool {
		t := time.Unix(key.(int64), 0).Truncate(period)
		if t.Equal(today) {
			cUint := value.(*atomic.Uint32)
			count += cUint.Load()
		}
		return true
	})
	return count
}

// SleepUntilReset will sleep until the binding of the given name's rate limit has reset, if the number of requests
// remaining is 0. No sleep will occur otherwise.
func (c *Client) SleepUntilReset(bindingName string) {
	rl := c.LatestRateLimit(bindingName)
	if rl != nil && rl.Reset().After(time.Now().UTC()) && rl.Remaining() == 0 {
		// If we have exceeded the rate limit, then we will wait until the reset time
		sleepDuration := rl.Reset().Sub(time.Now().UTC())
		c.Log(fmt.Sprintf(
			"We have reached the rate limit for %q, waiting for %s until %s",
			bindingName, sleepDuration.String(), rl.Reset().String(),
		))
		time.Sleep(sleepDuration)
	}
}

func (c *Client) Run(ctx context.Context, bindingName string, attrs map[string]any, req api.Request, res any) (err error) {
	request := req.(api.HTTPRequest).Request
	request.Header.Set("User-Agent", c.Config.RedditUserAgent())
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	//fmt.Printf("latest rate limit for %s = %v\n", bindingName, c.LatestRateLimit(bindingName))

	// If we are not currently fetching the AccessToken, then we will check if we need to refresh the access token.
	if bindingName != "access_token" {
		if err = c.RefreshToken(); err != nil {
			return
		}

		// Then we will set the headers from the AccessToken for this request
		headers := func() http.Header {
			c.accessTokenMutex.RLock()
			defer c.accessTokenMutex.RUnlock()
			return c.accessToken.Headers()
		}()

		for header, values := range headers {
			for _, value := range values {
				request.Header.Add(header, value)
			}
		}
	}

	//fmt.Printf("requesting %q = \"%s %s\"\n", bindingName, request.Method, request.URL.String())
	var response *http.Response
	if response, err = http.DefaultClient.Do(request); err != nil {
		err = errors.Wrapf(err, "could not make %s HTTP request to %s", request.Method, request.URL.String())
		return
	}

	if response.Body != nil {
		defer func(body io.ReadCloser) {
			err = myErrors.MergeErrors(err, errors.Wrapf(body.Close(),
				"could not close response body to %s %s",
				request.Method, request.URL.String(),
			))
		}(response.Body)
	}

	var body []byte
	if body, err = io.ReadAll(response.Body); err != nil {
		err = errors.Wrapf(err, "could not read response body to %s %s", request.Method, request.URL.String())
		return
	}

	if err = json.Unmarshal(body, res); err != nil {
		// Check to see if we can unmarshal the response body into an Error which we can return instead.
		var redditError Error
		if unmarshalErrorErr := json.Unmarshal(body, &redditError); unmarshalErrorErr == nil {
			redditError.request = request
			err = &redditError
			return
		}

		err = errors.Wrapf(err,
			"could not unmarshal JSON for response to %s %s",
			request.Method, request.URL.String(),
		)
		return
	}

	var rl api.RateLimit
	if rl, err = RateLimitFromHeader(response.Header); err != nil {
		err = errors.Wrapf(
			err, "could not parse RateLimit from response headers to %s %s",
			request.Method, request.URL.String(),
		)
		return
	}

	if rl.(*RateLimit) != nil {
		c.AddRateLimit(bindingName, rl)
		c.incrementRequestPeriod()
	}

	i := 0
	c.rateLimits.Range(func(key, value any) bool {
		c.Log(fmt.Sprintf("%d: Binding %q - %q", i+1, key, value))
		i++
		return true
	})

	i = 0
	c.requestsPeriodCount.Range(func(key, value any) bool {
		count := value.(*atomic.Uint32)
		c.Log(fmt.Sprintf(
			"%d: Period %q - %d", i+1,
			time.Unix(key.(int64), 0).Format("15:04:05"), count.Load(),
		))
		i++
		return true
	})

	return
}

func CreateClient(config Config) {
	var requestPeriods atomic.Uint32
	DefaultClient = &Client{
		Config:         config,
		requestPeriods: &requestPeriods,
	}
	API.Client = DefaultClient
}
