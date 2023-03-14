package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/andygello555/game-scout/api"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"strconv"
	"sync"
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
	return time.Now().Before(at.ExpireTime)
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
func RateLimitFromHeader(header http.Header) (rl *RateLimit, err error) {
	rl = &RateLimit{}
	if rl.remaining, err = strconv.ParseInt(header.Get("X-Ratelimit-Remaining"), 10, 64); err != nil {
		err = errors.Wrap(err, "cannot parse \"X-Ratelimit-Remaing\" header to int")
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
	rl.reset = time.Now().Add(time.Second * time.Duration(resetSeconds))
	return
}

func (r *RateLimit) Reset() time.Time        { return r.reset }
func (r *RateLimit) Remaining() int          { return int(r.remaining) }
func (r *RateLimit) Used() int               { return int(r.used) }
func (r *RateLimit) Type() api.RateLimitType { return api.RequestRateLimit }

func (r *RateLimit) String() string {
	return fmt.Sprintf(
		"%d/%d used (%.2f%%) resets %s",
		r.used, r.used+r.remaining, (float64(r.used)/(float64(r.used)+float64(r.remaining)))*100.0, r.reset.String(),
	)
}

type Client struct {
	Config      Config
	AccessToken *AccessToken
	// rateLimits is a sync.Map of binding names to references to api.RateLimit(s). I.e.
	//  map[string]api.RateLimit
	rateLimits sync.Map
}

func (c *Client) RateLimits() *sync.Map { return &c.rateLimits }

func (c *Client) AddRateLimit(bindingName string, rateLimit api.RateLimit) {
	if rlAny, ok := c.rateLimits.Load(bindingName); ok {
		// If there is already a RateLimit for this binding, check if the rate-limit returned by the current request is
		// newer.
		if rateLimit.Reset().After(rlAny.(api.RateLimit).Reset()) {
			c.rateLimits.Store(bindingName, rateLimit)
		}
	} else {
		c.rateLimits.Store(bindingName, rateLimit)
	}
}

func (c *Client) LatestRateLimit(bindingName string) api.RateLimit {
	var latestRateLimit api.RateLimit
	c.rateLimits.Range(func(key, value any) bool {
		rl := value.(api.RateLimit)
		if latestRateLimit == nil {
			latestRateLimit = rl
		} else {
			if latestRateLimit.Reset().Before(rl.Reset()) {
				latestRateLimit = rl
			}
		}
		return true
	})
	return latestRateLimit
}

func CreateClient(config Config) {
	DefaultClient = &Client{Config: config}
	API.Client = DefaultClient
}

func (c *Client) RefreshToken() (err error) {
	if c.AccessToken == nil || c.AccessToken.Expired() {
		if _, err = API.Execute("access_token"); err != nil {
			err = errors.Wrap(err, "could not fetched access_token for Reddit API")
		}
	}
	return
}

func (c *Client) Run(ctx context.Context, bindingName string, attrs map[string]any, req api.Request, res any) (err error) {
	request := req.(api.HTTPRequest).Request
	request.Header.Set("User-Agent", c.Config.RedditUserAgent())
	fmt.Printf("latest rate limit for %s = %v\n", bindingName, c.LatestRateLimit(bindingName))

	// If we are not currently fetching the AccessToken, then we will check if we need to refresh the access token.
	if bindingName != "access_token" {
		if err = c.RefreshToken(); err != nil {
			return
		}

		// Then we will set the headers from the AccessToken for this request
		for header, values := range c.AccessToken.Headers() {
			for _, value := range values {
				request.Header.Add(header, value)
			}
		}
	}
	//fmt.Println("requesting", request.Method, request.URL.String(), request.Header)

	var response *http.Response
	if response, err = http.DefaultClient.Do(request); err != nil {
		err = errors.Wrapf(err, "could not make %s HTTP request to %s", request.Method, request.URL.String())
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
	c.AddRateLimit(bindingName, rl)

	i := 0
	c.rateLimits.Range(func(key, value any) bool {
		fmt.Printf("%d: %s - %q", i+1, key, value)
		i++
		return true
	})
	fmt.Println()

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
		err = errors.Wrapf(err,
			"could not unmarshal JSON for response to %s %s",
			request.Method, request.URL.String(),
		)
	}
	return
}
