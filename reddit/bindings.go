package reddit

import (
	"fmt"
	"github.com/andygello555/game-scout/api"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var API = api.NewAPI(nil, api.Schema{
	"access_token": api.WrapBinding("access_token", api.NewBindingChain(func(binding api.Binding[accessTokenResponse, AccessToken], args ...any) (request api.Request) {
		client := binding.Attrs()["client"].(*Client)
		data := url.Values{
			"grant_type": {"password"},
			"username":   {client.Config.RedditUsername()},
			"password":   {client.Config.RedditPassword()},
		}
		req, _ := http.NewRequest(http.MethodPost, "https://www.reddit.com/api/v1/access_token", strings.NewReader(data.Encode()))
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(client.Config.RedditPersonalUseScript(), client.Config.RedditSecret())
		return api.HTTPRequest{Request: req}
	}).SetResponseMethod(func(binding api.Binding[accessTokenResponse, AccessToken], response accessTokenResponse, args ...any) AccessToken {
		client := binding.Attrs()["client"].(*Client)
		client.AccessToken = &AccessToken{
			accessTokenResponse: response,
			FetchedTime:         time.Now(),
			ExpireTime:          time.Now().Add(time.Second * time.Duration(response.ExpiresIn)),
		}
		return *client.AccessToken
	}).AddAttrs(
		func(client api.Client) (string, any) { return "client", client },
		func(client api.Client) (string, any) { return "access_token", true },
		func(client api.Client) (string, any) { return "binding", "access_token" },
	)),

	"me": api.WrapBinding("me", api.NewBindingChain(func(binding api.Binding[Me, Me], args ...any) (request api.Request) {
		req, _ := http.NewRequest(http.MethodGet, "https://oauth.reddit.com/api/v1/me", http.NoBody)
		return api.HTTPRequest{Request: req}
	}).AddAttrs(
		func(client api.Client) (string, any) { return "binding", "me" },
	)),

	"top": api.WrapBinding("top", api.NewBindingChain(func(binding api.Binding[listingWrapper, Listing], args ...any) (request api.Request) {
		subreddit := args[0].(string)
		timePeriod := args[1].(TimePeriod)
		req, _ := http.NewRequest(
			http.MethodGet,
			fmt.Sprintf(
				"https://oauth.reddit.com/r/%s/top.json?t=%s",
				subreddit, timePeriod,
			),
			http.NoBody,
		)
		return api.HTTPRequest{Request: req}
	}).SetResponseMethod(func(binding api.Binding[listingWrapper, Listing], response listingWrapper, args ...any) Listing {
		return response.Data
	}).SetParamsMethod(func(binding api.Binding[listingWrapper, Listing]) []api.BindingParam {
		return api.Params("subreddit", "", true, "timePeriod", Day)
	}).AddAttrs(
		func(client api.Client) (string, any) { return "binding", "top" },
	)),
})
