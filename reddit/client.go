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

type Client struct {
	Config      Config
	AccessToken *AccessToken
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

	// If we are not currently fetching the AccessToken, then we will check if we need to refresh the access token.
	if _, ok := attrs["access_token"]; !ok {
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

	var response *http.Response
	if response, err = http.DefaultClient.Do(request); err != nil {
		err = errors.Wrapf(err, "could not make %s HTTP request to %s", request.Method, request.URL.String())
		return
	}

	if response.Body != nil {
		defer func(body io.ReadCloser) {
			err = myErrors.MergeErrors(err, errors.Wrapf(body.Close(), "could not close response body to %s", request.URL.String()))
		}(response.Body)
	}

	var body []byte
	if body, err = io.ReadAll(response.Body); err != nil {
		err = errors.Wrapf(err, "could not read response body to %s %s", request.Method, request.URL.String())
		return
	}

	if err = json.Unmarshal(body, res); err != nil {
		err = errors.Wrapf(err, "could not unmarshal JSON for response to %s %s", request.Method, request.URL.String())
	}
	return
}

func CreateClient(config Config) {
	DefaultClient = &Client{Config: config}
	API.Client = DefaultClient
}
