package twitter

import (
	"fmt"
	"github.com/g8rswimmer/go-twitter/v2"
	"net/http"
)

var Client *twitter.Client

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
}

func ClientCreate(config Config) {
	Client = &twitter.Client{
		Authorizer: authorize{
			Token: config.TwitterBearerToken(),
		},
		Client: http.DefaultClient,
		Host:   "https://api.twitter.com",
	}
}
