package reddit

import (
	"fmt"
	"github.com/andygello555/gapi"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var API = api.NewAPI(nil, api.Schema{
	"access_token": api.WrapBinding(api.NewBindingChain(func(binding api.Binding[accessTokenResponse, AccessToken], args ...any) (request api.Request) {
		client := binding.Attrs()["client"].(*Client)
		data := url.Values{
			"grant_type": {"password"},
			"username":   {client.Config.RedditUsername()},
			"password":   {client.Config.RedditPassword()},
		}
		req, _ := http.NewRequest(http.MethodPost, "https://www.reddit.com/api/v1/access_token", strings.NewReader(data.Encode()))
		req.SetBasicAuth(client.Config.RedditPersonalUseScript(), client.Config.RedditSecret())
		return api.HTTPRequest{Request: req}
	}).SetResponseMethod(func(binding api.Binding[accessTokenResponse, AccessToken], response accessTokenResponse, args ...any) AccessToken {
		client := binding.Attrs()["client"].(*Client)
		client.AccessToken = &AccessToken{
			accessTokenResponse: response,
			FetchedTime:         time.Now().UTC(),
			ExpireTime:          time.Now().UTC().Add(time.Second * time.Duration(response.ExpiresIn)),
		}
		return *client.AccessToken
	}).AddAttrs(
		func(client api.Client) (string, any) { return "client", client },
	).SetName("access_token")),

	"me": api.WrapBinding(api.NewBindingChain(func(binding api.Binding[Me, Me], args ...any) (request api.Request) {
		req, _ := http.NewRequest(http.MethodGet, "https://oauth.reddit.com/api/v1/me", http.NoBody)
		return api.HTTPRequest{Request: req}
	}).SetName("me")),

	"top": api.WrapBinding(api.NewBindingChain(func(binding api.Binding[listingWrapper, *Listing], args ...any) (request api.Request) {
		subreddit := args[0].(string)
		timePeriod := args[1].(TimePeriod)
		limit := args[2].(int)
		after := args[3].(string)
		req, _ := http.NewRequest(
			http.MethodGet,
			fmt.Sprintf(
				"https://oauth.reddit.com/r/%s/top.json?t=%s&limit=%d&after=%s",
				subreddit, timePeriod, limit, after,
			),
			http.NoBody,
		)
		return api.HTTPRequest{Request: req}
	}).SetResponseMethod(func(binding api.Binding[listingWrapper, *Listing], response listingWrapper, args ...any) *Listing {
		return &response.Data
	}).SetParamsMethod(func(binding api.Binding[listingWrapper, *Listing]) []api.BindingParam {
		return api.Params(
			"subreddit", "", true,
			"timePeriod", Day,
			"limit", 25,
			"after", "",
		)
	}).SetPaginated(true).SetName("top")),

	"comments": api.WrapBinding(api.NewBindingChain(func(binding api.Binding[commentResponse, *PostAndComments], args ...any) (request api.Request) {
		article := args[0].(string)
		subreddit := args[1].(*string)
		pc := args[2].(*PostAndComments)

		subredditURL := ""
		if subreddit != nil {
			subredditURL = fmt.Sprintf("r/%s/", *subreddit)
		}

		// If we are not requesting more comments from an existing thread then we will fetch the comments for a thread.
		if pc == nil {
			req, _ := http.NewRequest(
				http.MethodGet,
				fmt.Sprintf("https://oauth.reddit.com/%scomments/%s.json", subredditURL, article),
				http.NoBody,
			)
			return api.HTTPRequest{Request: req}
		}

		postID := pc.Post.FullID
		commentIDs := pc.More.Children

		form := url.Values{}
		form.Set("api_type", "json")
		form.Set("link_id", postID)
		form.Set("children", strings.Join(commentIDs, ","))

		// This was originally a GET, but with POST you can send a bigger payload
		// since it's in the body and not the URI.
		req, _ := http.NewRequest(http.MethodPost, "https://oauth.reddit.com/api/morechildren.json", strings.NewReader(form.Encode()))
		return api.HTTPRequest{Request: req}
	}).SetResponseMethod(func(binding api.Binding[commentResponse, *PostAndComments], response commentResponse, args ...any) *PostAndComments {
		pc := args[2].(*PostAndComments)
		if pc != nil {
			// We merge the mores back into the PostAndComments that was passed in through args
			response.postAndComments = pc
			comments := response.more.JSON.Data.Things.Comments
			for _, c := range comments {
				response.postAndComments.addCommentToTree(c)
			}

			noMore := true

			mores := response.more.JSON.Data.Things.Mores
			for _, m := range mores {
				if strings.HasPrefix(m.ParentID, kindPost+"_") {
					noMore = false
				}
				response.postAndComments.addMoreToTree(m)
			}

			if noMore {
				response.postAndComments.More = nil
			}
		}
		return response.postAndComments
	}).SetParamsMethod(func(binding api.Binding[commentResponse, *PostAndComments]) []api.BindingParam {
		return api.Params(
			"article", "", true,
			"subreddit", (*string)(nil),
			"after", (*PostAndComments)(nil),
		)
	}).SetPaginated(true).SetName("comments")),

	"user_about": api.WrapBinding(api.NewBindingChain(func(binding api.Binding[Thing, *User], args ...any) (request api.Request) {
		username := args[0].(string)
		req, _ := http.NewRequest(
			http.MethodGet,
			fmt.Sprintf("https://oauth.reddit.com/user/%s/about.json", username),
			http.NoBody,
		)
		return api.HTTPRequest{Request: req}
	}).SetResponseMethod(func(binding api.Binding[Thing, *User], response Thing, args ...any) *User {
		return response.Data.(*User)
	}).SetParamsMethod(func(binding api.Binding[Thing, *User]) []api.BindingParam {
		return api.Params("username", "", true)
	}).SetName("user_about")),

	"user_where": api.WrapBinding(api.NewBindingChain(func(binding api.Binding[listingWrapper, *Listing], args ...any) (request api.Request) {
		username := args[0].(string)
		where := args[1].(UserWhereType)
		sort := args[2].(Sort)
		timePeriod := args[3].(TimePeriod)
		limit := args[4].(int)
		after := args[5].(string)

		req, _ := http.NewRequest(
			http.MethodGet,
			fmt.Sprintf(
				"https://oauth.reddit.com/user/%s/%s.json?sort=%s&t=%s&limit=%d&after=%s",
				username, where, sort, timePeriod, limit, after,
			),
			http.NoBody,
		)
		return api.HTTPRequest{Request: req}
	}).SetResponseMethod(func(binding api.Binding[listingWrapper, *Listing], response listingWrapper, args ...any) *Listing {
		return &response.Data
	}).SetParamsMethod(func(binding api.Binding[listingWrapper, *Listing]) []api.BindingParam {
		return api.Params(
			"username", "", true,
			"where", Overview, true,
			"sort", Hot,
			"timePeriod", Day,
			"limit", 25,
			"after", "",
		)
	}).SetPaginated(true).SetName("user_where")),
})
