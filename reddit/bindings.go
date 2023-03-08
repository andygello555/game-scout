package reddit

import (
	"bytes"
	"encoding/json"
	"github.com/andygello555/game-scout/api"
	"net/http"
	"time"
)

var API = api.NewAPI(nil, api.Schema{
	"access_token": api.NewWrappedBinding[accessTokenResponse, AccessToken]("access_token",
		func(binding api.Binding[accessTokenResponse, AccessToken], args ...any) (request api.Request) {
			client := binding.Attrs()["client"].(*Client)
			data := map[string]string{
				"grant_type": "password",
				"username":   client.Config.RedditUsername(),
				"password":   client.Config.RedditPassword(),
			}
			cred, _ := json.Marshal(data)
			req, _ := http.NewRequest(http.MethodPost, "https://www.reddit.com/api/v1/access_token", bytes.NewBuffer(cred))
			req.SetBasicAuth(client.Config.RedditPersonalUseScript(), client.Config.RedditSecret())
			return api.HTTPRequest{Request: req}
		}, nil, nil,
		func(binding api.Binding[accessTokenResponse, AccessToken], response accessTokenResponse, args ...any) AccessToken {
			client := binding.Attrs()["client"].(*Client)
			client.AccessToken = &AccessToken{
				accessTokenResponse: response,
				FetchedTime:         time.Now(),
				ExpireTime:          time.Now().Add(time.Second * time.Duration(response.ExpiresIn)),
			}
			return *client.AccessToken
		}, false,
		func(client api.Client) (string, any) { return "client", client },
		func(client api.Client) (string, any) { return "access_token", true },
	),
	"me": api.NewWrappedBinding[Me, Me]("me",
		func(binding api.Binding[Me, Me], args ...any) (request api.Request) {
			req, _ := http.NewRequest(http.MethodGet, "https://oauth.reddit.com/api/v1/me", http.NoBody)
			return api.HTTPRequest{Request: req}
		}, nil, nil, nil, false,
	),
})
