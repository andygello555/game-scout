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
	Used      int64
	Remaining int64
	Reset     time.Time
}

// RateLimitFromHeader returns a new RateLimit instance from the given http.Header by fetching and parsing the values
// for the following headers:
//   - X-Ratelimit-Remaining
//   - X-Ratelimit-Reset
//   - X-Ratelimit-Used
func RateLimitFromHeader(header http.Header) (rl *RateLimit, err error) {
	rl = &RateLimit{}
	if rl.Remaining, err = strconv.ParseInt(header.Get("X-Ratelimit-Remaining"), 10, 64); err != nil {
		err = errors.Wrap(err, "cannot parse \"X-Ratelimit-Remaing\" header to int")
		return
	}

	if rl.Used, err = strconv.ParseInt(header.Get("X-Ratelimit-Used"), 10, 64); err != nil {
		err = errors.Wrap(err, "cannot parse \"X-Ratelimit-Used\" header to int")
		return
	}

	var resetSeconds int64
	if resetSeconds, err = strconv.ParseInt(header.Get("X-Ratelimit-Reset"), 10, 64); err != nil {
		err = errors.Wrap(err, "cannot \"X-Ratelimit-Reset\" header to int")
		return
	}
	rl.Reset = time.Now().Add(time.Second * time.Duration(resetSeconds))
	return
}

type Client struct {
	Config      Config
	AccessToken *AccessToken
	RateLimits  sync.Map
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

func (c *Client) Run(ctx context.Context, attrs map[string]any, req api.Request, res any) (err error) {
	request := req.(api.HTTPRequest).Request
	request.Header.Set("User-Agent", c.Config.RedditUserAgent())
	binding := attrs["binding"].(string)

	// If we are not currently fetching the AccessToken, then we will check if we need to refresh the access token.
	if binding != "access_token" {
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

	var rl *RateLimit
	if rl, err = RateLimitFromHeader(response.Header); err != nil {
		err = errors.Wrapf(
			err, "could not parse RateLimit from response headers to %s %s",
			request.Method, request.URL.String(),
		)
		return
	}

	if rlAny, ok := c.RateLimits.Load(binding); ok {
		// If there is already a RateLimit for this binding, check if the rate-limit returned by the current request is
		// newer.
		if rl.Reset.After(rlAny.(*RateLimit).Reset) {
			c.RateLimits.Store(binding, rl)
		}
	} else {
		c.RateLimits.Store(binding, rl)
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
		err = errors.Wrapf(err,
			"could not unmarshal JSON for response to %s %s",
			request.Method, request.URL.String(),
		)
	}
	return
}
