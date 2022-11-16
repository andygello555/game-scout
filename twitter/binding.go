package twitter

import (
	"context"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/andygello555/game-scout/errors"
	"github.com/g8rswimmer/go-twitter/v2"
	"strings"
	"time"
)

// BindingResourceType represents the resource type that is returned by a Binding.
type BindingResourceType int

const (
	// Tweet is returned by Bindings that fetch tweets.
	Tweet BindingResourceType = iota
	User
)

// String returns the name of the BindingResourceType.
func (brt BindingResourceType) String() string {
	switch brt {
	case Tweet:
		return "Tweet"
	case User:
		return "User"
	default:
		return "<nil>"
	}
}

// BindingType represents a binding for the ClientWrapper.
type BindingType int

const (
	// RecentSearch binds to the twitter.Client.TweetRecentSearch method.
	RecentSearch BindingType = iota
	// UserRetrieve binds to the twitter.Client.UserLookup method.
	UserRetrieve
	// UserNameRetrieve binds to the twitter.Client.UserNameLookup method.
	UserNameRetrieve
)

// String returns the name of the BindingType.
func (bt BindingType) String() string {
	switch bt {
	case RecentSearch:
		return "RecentSearch"
	case UserRetrieve:
		return "UserRetrieve"
	case UserNameRetrieve:
		return "UserNameRetrieve"
	default:
		return "<nil>"
	}
}

// Binding returns the Binding for the BindingType using the clientBindings.
func (bt BindingType) Binding() *Binding {
	binding := clientBindings[bt]
	return &binding
}

// WrapResponse wraps the given response from twitter.Client in a prototype that implements the BindingResult interface.
func (bt BindingType) WrapResponse(response any) (result BindingResult, err error) {
	bindingResult := &struct{ bindingResultProto }{}
	switch bt {
	case RecentSearch:
		tweetRecentSearchResponse := response.(*twitter.TweetRecentSearchResponse)
		// If any errors have occurred then we'll treat them as Go errors by wrapping them in a bindingErrorProto
		if len(tweetRecentSearchResponse.Raw.Errors) > 0 {
			// If we find any errors that shouldn't be ignored we will return that error
			ignore := true
			for _, tweetErr := range tweetRecentSearchResponse.Raw.Errors {
				if !strings.Contains(IgnoredErrorTypes, tweetErr.Type) {
					log.ERROR.Printf(
						"Un-ignorable occurred in %s response: %s, %s", bt.String(), tweetErr.Type, tweetErr.Detail,
					)
					ignore = false
					break
				}
			}
			if !ignore {
				bep := &bindingErrorProto{errorsMethod: func() []*twitter.ErrorObj { return tweetRecentSearchResponse.Raw.Errors }}
				return bindingResult, bep
			}
		}
		bindingResult.rawMethod = func() any { return tweetRecentSearchResponse.Raw }
		bindingResult.metaMethod = func() BindingResultMeta {
			resultMeta := &struct{ bindingResultMetaProto }{}
			resultMeta.newestIDMethod = func() string { return tweetRecentSearchResponse.Meta.NewestID }
			resultMeta.oldestIDMethod = func() string { return tweetRecentSearchResponse.Meta.OldestID }
			resultMeta.resultCountMethod = func() int { return tweetRecentSearchResponse.Meta.ResultCount }
			resultMeta.nextTokenMethod = func() string { return tweetRecentSearchResponse.Meta.NextToken }
			return resultMeta
		}
		bindingResult.rateLimitMethod = func() *twitter.RateLimit { return tweetRecentSearchResponse.RateLimit }
	case UserRetrieve, UserNameRetrieve:
		userLookupResponse := response.(*twitter.UserLookupResponse)
		bindingResult.rawMethod = func() any { return userLookupResponse.Raw }
		bindingResult.rateLimitMethod = func() *twitter.RateLimit { return userLookupResponse.RateLimit }
	default:
		return nil, fmt.Errorf("cannot find BindingType no. %d", bt)
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
	Request                     func(client *ClientWrapper, options *BindingOptions, args ...any) (any, error)
	RequestRateLimit            RequestRateLimit
	MinResourcesPerRequest      int
	MaxResourcesPerRequest      int
	Type                        BindingType
	ResourceType                BindingResourceType
	SetOptionsForNextRequest    func(previousResult BindingResult, args ...any) ([]any, error)
	SetOptionsForCurrentRequest func(resources int, args ...any) ([]any, error)
	ClampMaxResults             func(binding *Binding, args ...any) (int, []any)
}

// BindingResultMeta is an interface for the Meta field that can be found in many of the responses from twitter.Client.
type BindingResultMeta interface {
	// NewestID returns the newest ID in the response.
	NewestID() string
	// OldestID returns the oldest ID in the response.
	OldestID() string
	// ResultCount returns the result count in the response.
	ResultCount() int
	// NextToken returns the next token in the response for pagination.
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
	Raw() any
	Meta() BindingResultMeta
	RateLimit() *twitter.RateLimit
	MergeNext(nextBindingResult BindingResult, binding *Binding) (err error)
}

// bindingResultProto is the prototype for BindingResult that implements BindingResult.
type bindingResultProto struct {
	rawMethod       func() any
	metaMethod      func() BindingResultMeta
	rateLimitMethod func() *twitter.RateLimit
}

func (brp *bindingResultProto) Raw() any                      { return brp.rawMethod() }
func (brp *bindingResultProto) Meta() BindingResultMeta       { return brp.metaMethod() }
func (brp *bindingResultProto) RateLimit() *twitter.RateLimit { return brp.rateLimitMethod() }

// MergeNext will merge the referred to bindingResultProto with the next BindingResult. This will affectively accumulate
// the results into a new BindingResult and set the referred to bindingResultProto to this new BindingResult.
func (brp *bindingResultProto) MergeNext(nextBindingResult BindingResult, binding *Binding) (err error) {
	newBindingResult := &bindingResultProto{}
	createRawError := func() error {
		return errors.TemporaryErrorf(
			false,
			"cannot merge BindingResults for %s as one/both do(es) not have a \"Raw\" field",
			binding.Type.String(),
		)
	}

	switch binding.ResourceType {
	case Tweet:
		// First we check if there are twitter.TweetRaw to merge in both
		raw1 := brp.Raw().(*twitter.TweetRaw)
		raw2 := nextBindingResult.Raw().(*twitter.TweetRaw)
		if raw1 != nil && raw2 != nil {
			newRaw := &twitter.TweetRaw{
				Tweets: make([]*twitter.TweetObj, len(raw1.Tweets)+len(raw2.Tweets)),
				Includes: &twitter.TweetRawIncludes{
					Tweets: make([]*twitter.TweetObj, len(raw1.Includes.Tweets)+len(raw2.Includes.Tweets)),
					Users:  make([]*twitter.UserObj, len(raw1.Includes.Users)+len(raw2.Includes.Users)),
					Places: make([]*twitter.PlaceObj, len(raw1.Includes.Places)+len(raw2.Includes.Places)),
					Media:  make([]*twitter.MediaObj, len(raw1.Includes.Media)+len(raw2.Includes.Media)),
					Polls:  make([]*twitter.PollObj, len(raw1.Includes.Polls)+len(raw2.Includes.Polls)),
				},
				Errors: make([]*twitter.ErrorObj, len(raw1.Errors)+len(raw2.Errors)),
			}
			var i int
			// Filling Tweets field in the root
			for i = 0; i < len(raw1.Tweets)+len(raw2.Tweets); i++ {
				readIdx := i
				read := &raw1.Tweets
				if i >= len(raw1.Tweets) {
					readIdx = i - len(raw1.Tweets)
					read = &raw2.Tweets
				}
				newRaw.Tweets[i] = (*read)[readIdx]
			}
			// Filling Includes.Tweets
			for i = 0; i < len(raw1.Includes.Tweets)+len(raw2.Includes.Tweets); i++ {
				readIdx := i
				read := &raw1.Includes.Tweets
				if i >= len(raw1.Includes.Tweets) {
					readIdx = i - len(raw1.Includes.Tweets)
					read = &raw2.Includes.Tweets
				}
				newRaw.Includes.Tweets[i] = (*read)[readIdx]
			}
			// Filling Includes.Users
			for i = 0; i < len(raw1.Includes.Users)+len(raw2.Includes.Users); i++ {
				readIdx := i
				read := &raw1.Includes.Users
				if i >= len(raw1.Includes.Users) {
					readIdx = i - len(raw1.Includes.Users)
					read = &raw2.Includes.Users
				}
				newRaw.Includes.Users[i] = (*read)[readIdx]
			}
			// Filling Includes.Places
			for i = 0; i < len(raw1.Includes.Places)+len(raw2.Includes.Places); i++ {
				readIdx := i
				read := &raw1.Includes.Places
				if i >= len(raw1.Includes.Places) {
					readIdx = i - len(raw1.Includes.Places)
					read = &raw2.Includes.Places
				}
				newRaw.Includes.Places[i] = (*read)[readIdx]
			}
			// Filling Includes.Media
			for i = 0; i < len(raw1.Includes.Media)+len(raw2.Includes.Media); i++ {
				readIdx := i
				read := &raw1.Includes.Media
				if i >= len(raw1.Includes.Media) {
					readIdx = i - len(raw1.Includes.Media)
					read = &raw2.Includes.Media
				}
				newRaw.Includes.Media[i] = (*read)[readIdx]
			}
			// Filling Includes.Polls
			for i = 0; i < len(raw1.Includes.Polls)+len(raw2.Includes.Polls); i++ {
				readIdx := i
				read := &raw1.Includes.Polls
				if i >= len(raw1.Includes.Polls) {
					readIdx = i - len(raw1.Includes.Polls)
					read = &raw2.Includes.Polls
				}
				newRaw.Includes.Polls[i] = (*read)[readIdx]
			}
			// Filling Errors field
			for i = 0; i < len(raw1.Errors)+len(raw2.Errors); i++ {
				readIdx := i
				read := &raw1.Errors
				if i >= len(raw1.Errors) {
					readIdx = i - len(raw1.Errors)
					read = &raw2.Errors
				}
				newRaw.Errors[i] = (*read)[readIdx]
			}
			newBindingResult.rawMethod = func() any { return newRaw }
		} else {
			return createRawError()
		}
	case User:
		raw1 := brp.Raw().(*twitter.UserRaw)
		raw2 := nextBindingResult.Raw().(*twitter.UserRaw)
		if raw1 != nil && raw2 != nil {
			newRaw := &twitter.UserRaw{
				Users:    make([]*twitter.UserObj, len(raw1.Users)+len(raw2.Users)),
				Includes: &twitter.UserRawIncludes{Tweets: make([]*twitter.TweetObj, len(raw1.Includes.Tweets)+len(raw2.Includes.Tweets))},
				Errors:   make([]*twitter.ErrorObj, len(raw1.Errors)+len(raw2.Errors)),
			}
			var i int
			// Filling Users field in the root
			for i = 0; i < len(raw1.Users)+len(raw2.Users); i++ {
				readIdx := i
				read := &raw1.Users
				if i >= len(raw1.Users) {
					readIdx = i - len(raw1.Users)
					read = &raw2.Users
				}
				newRaw.Users[i] = (*read)[readIdx]
			}
			// Filling Includes.Tweets
			for i = 0; i < len(raw1.Includes.Tweets)+len(raw2.Includes.Tweets); i++ {
				readIdx := i
				read := &raw1.Includes.Tweets
				if i >= len(raw1.Includes.Tweets) {
					readIdx = i - len(raw1.Includes.Tweets)
					read = &raw2.Includes.Tweets
				}
				newRaw.Includes.Tweets[i] = (*read)[readIdx]
			}
			// Filling Errors field in the root
			for i = 0; i < len(raw1.Errors)+len(raw2.Errors); i++ {
				readIdx := i
				read := &raw1.Errors
				if i >= len(raw1.Errors) {
					readIdx = i - len(raw1.Errors)
					read = &raw2.Errors
				}
				newRaw.Errors[i] = (*read)[readIdx]
			}
			newBindingResult.rawMethod = func() any { return newRaw }
		} else {
			return createRawError()
		}
	}

	// Then we check if there are Meta fields for both bindingResultProtos. If there are, then we will take the
	// nextBindingResult's Meta value as the new value.
	meta1 := brp.Meta()
	meta2 := nextBindingResult.Meta()
	if meta1 != nil && meta2 != nil {
		newBindingResult.metaMethod = func() BindingResultMeta {
			resultMeta := &struct{ bindingResultMetaProto }{}
			resultMeta.newestIDMethod = func() string { return meta2.NewestID() }
			resultMeta.oldestIDMethod = func() string { return meta2.OldestID() }
			// resultCountMethod returns the sum of the previous meta's ResultCount() result and the next meta's
			// ResultCount() result.
			resultMeta.resultCountMethod = func() int { return meta1.ResultCount() + meta2.ResultCount() }
			resultMeta.nextTokenMethod = func() string { return meta2.NextToken() }
			return resultMeta
		}
	} else {
		return errors.TemporaryErrorf(
			false,
			"cannot merge BindingResults as one/both do(es) not have a \"Meta\" field",
		)
	}

	// Then we check if there are RateLimit fields for both bindingResultProtos. If there are, then we will take the
	// nextBindingResult's RateLimit value as the new value.
	rateLimit1 := brp.RateLimit()
	rateLimit2 := nextBindingResult.RateLimit()
	if rateLimit1 != nil && rateLimit2 != nil {
		newBindingResult.rateLimitMethod = func() *twitter.RateLimit { return rateLimit2 }
	} else {
		return errors.TemporaryErrorf(
			false,
			"cannot merge BindingResults as one/both do(es) not have a \"RateLimit\" field",
		)
	}
	// Finally, we set the current referrer to be the new bindingResultProto
	*brp = *newBindingResult
	return nil
}

// BindingError wraps an array of twitter.ErrorObj in a Go error.
type BindingError interface {
	error
	Errors() []*twitter.ErrorObj
}

// bindingErrorProto is the prototype for BindingError that implements BindingError.
type bindingErrorProto struct {
	// errorsMethod is a function that returns an array of twitter.ErrorObj.
	errorsMethod func() []*twitter.ErrorObj
}

func (bep *bindingErrorProto) Errors() []*twitter.ErrorObj { return bep.errorsMethod() }

// Error serialises each twitter.ErrorObj and returns a string of all serialised objects.
func (bep *bindingErrorProto) Error() string {
	var b strings.Builder
	errors := bep.Errors()
	for i, errObj := range errors {
		b.WriteString(fmt.Sprintf(
			"{Title: \"%s\", Type: \"%s\", RType: \"%s\", Val: %v, Det: \"%s\", Param: \"%s\"}",
			errObj.Title, errObj.Type, errObj.ResourceType, errObj.Value, errObj.Detail, errObj.Parameter,
		))
		if i != len(errors)-1 {
			b.WriteString(", ")
		}
	}
	return fmt.Sprintf("API errors occurred: [%s]", b.String())
}

// BindingOptions are the options available for the execution of a Binding.
type BindingOptions struct {
	Total int
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
		RequestRateLimit:       RequestRateLimit{Requests: 450, Every: time.Minute * 15},
		Type:                   RecentSearch,
		ResourceType:           Tweet,
		MinResourcesPerRequest: 10,
		MaxResourcesPerRequest: 100,
		SetOptionsForNextRequest: func(previousResult BindingResult, args ...any) ([]any, error) {
			options := args[1].(twitter.TweetRecentSearchOpts)
			options.NextToken = previousResult.Meta().NextToken()
			args[1] = options
			return args, nil
		},
		SetOptionsForCurrentRequest: func(resources int, args ...any) ([]any, error) {
			options := args[1].(twitter.TweetRecentSearchOpts)
			options.MaxResults = resources
			args[1] = options
			return args, nil
		},
		ClampMaxResults: func(binding *Binding, args ...any) (int, []any) {
			maxResults := 0
			options := args[1].(twitter.TweetRecentSearchOpts)
			if options.MaxResults < binding.MinResourcesPerRequest {
				options.MaxResults = binding.MinResourcesPerRequest
				maxResults = binding.MinResourcesPerRequest
			} else if options.MaxResults > binding.MaxResourcesPerRequest {
				options.MaxResults = binding.MaxResourcesPerRequest
				maxResults = binding.MaxResourcesPerRequest
			} else {
				maxResults = options.MaxResults
			}
			args[1] = options
			return maxResults, args
		},
	},
	UserRetrieve: {
		Request: func(client *ClientWrapper, options *BindingOptions, args ...any) (response any, err error) {
			if response, err = client.Client.UserLookup(
				context.Background(),
				args[0].([]string),
				args[1].(twitter.UserLookupOpts),
			); err != nil {
				return response, err
			}
			return response, nil
		},
		RequestRateLimit:            RequestRateLimit{Requests: 300, Every: time.Minute * 15},
		Type:                        UserRetrieve,
		ResourceType:                User,
		MinResourcesPerRequest:      1,
		MaxResourcesPerRequest:      1,
		SetOptionsForNextRequest:    nil,
		SetOptionsForCurrentRequest: nil,
		ClampMaxResults: func(binding *Binding, args ...any) (int, []any) {
			return 1, args
		},
	},
	UserNameRetrieve: {
		Request: func(client *ClientWrapper, options *BindingOptions, args ...any) (response any, err error) {
			if response, err = client.Client.UserNameLookup(
				context.Background(),
				args[0].([]string),
				args[1].(twitter.UserLookupOpts),
			); err != nil {
				return response, err
			}
			return response, nil
		},
		RequestRateLimit:            RequestRateLimit{Requests: 300, Every: time.Minute * 15},
		Type:                        UserNameRetrieve,
		ResourceType:                User,
		MinResourcesPerRequest:      1,
		MaxResourcesPerRequest:      1,
		SetOptionsForNextRequest:    nil,
		SetOptionsForCurrentRequest: nil,
		ClampMaxResults: func(binding *Binding, args ...any) (int, []any) {
			return 1, args
		},
	},
}
