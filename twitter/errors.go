package twitter

import (
	"fmt"
	"github.com/g8rswimmer/go-twitter/v2"
	"time"
)

// RateLimitError represents an error returned by ClientWrapper.CheckRateLimit.
type RateLimitError struct {
	// RateLimit is a copy of the rate limit from a Twitter API action response.
	RateLimit *twitter.RateLimit
	// TweetCap is a copy of the TweetCap from the ClientWrapper.
	TweetCap *TweetCap
	// RequestRateLimit is a copy of the RequestRateLimit struct from a Binding.
	RequestRateLimit *RequestRateLimit
	// MaxResourcesPerRequest is the maximum number of resources possible to be requested from a single request of a
	// Binding.
	MaxResourcesPerRequest int
	// RequestedResources is the total number of resources that were requested.
	RequestedResources int
	// ResourceType is the BindingResourceType of the Binding that failed the ClientWrapper.CheckRateLimit check.
	ResourceType BindingResourceType
	// Happened is when the error happened.
	Happened time.Time
}

func (rle *RateLimitError) Error() string {
	switch {
	case rle.RateLimit != nil:
		if rle.RequestedResources > rle.RateLimit.Remaining*rle.MaxResourcesPerRequest {
			return fmt.Sprintf(
				"cannot request %d %ss as this exceeds the rate limit saved from an earlier request %d/%d"+
					" resets %s",
				rle.RequestedResources, rle.ResourceType.String(), rle.RateLimit.Remaining, rle.RateLimit.Limit,
				rle.RateLimit.Reset.Time().String(),
			)
		}
		if remaining := rle.RateLimit.Reset.Time().Sub(rle.Happened) / TimePerRequest; int64(remaining) < int64(rle.RequestedResources/rle.MaxResourcesPerRequest) {
			return fmt.Sprintf(
				"cannot request now (%s) as the number of requests to make to get %d %ss exceeds the number we can make in the time remaining %s",
				rle.Happened.String(),
				rle.RequestedResources,
				rle.ResourceType.String(),
				remaining.String(),
			)
		}
	case rle.TweetCap != nil:
		return fmt.Sprintf("cannot request %d Tweets as our Tweet cap is %s", rle.RequestedResources, rle.TweetCap.String())
	case rle.RequestRateLimit != nil:
		return fmt.Sprintf(
			"cannot request %d %ss as the maximum for this action is %s (%d %ss)",
			rle.RequestedResources,
			rle.ResourceType.String(),
			rle.RequestRateLimit.String(),
			rle.RequestRateLimit.Requests*rle.MaxResourcesPerRequest,
			rle.ResourceType.String(),
		)
	}
	return ""
}
