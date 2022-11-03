package twitter

import (
	"fmt"
	"github.com/g8rswimmer/go-twitter/v2"
	"time"
)

// temporary indicates whether an error is temporary
type temporary interface {
	Temporary() bool
}

// temporaryProto acts as a prototype for anonymous structs to implement the temporary interface.
type temporaryProto struct {
	temporaryMethod func() bool
	msgMethod       func() string
	causeMethod     func() error
}

// Temporary calls the temporaryMethod of the prototype, thus implementing the temporary interface.
func (tp temporaryProto) Temporary() bool {
	return tp.temporaryMethod()
}

// Error returns the outcome of the msgMethod of the prototype if there is no causeMethod. Otherwise, the result of
// msgMethod will be concatenated with causeMethod.
func (tp temporaryProto) Error() string {
	if tp.causeMethod == nil {
		return tp.msgMethod()
	} else {
		return tp.msgMethod() + ": " + tp.causeMethod().Error()
	}
}

// Cause calls the causeMethod of the prototype, thus implementing the causer interface.
func (tp temporaryProto) Cause() error {
	return tp.causeMethod()
}

// IsTemporary returns true if err is temporary.
func IsTemporary(err error) bool {
	te, ok := err.(temporary)
	return ok && te.Temporary()
}

// TemporaryErrorf creates a new temporary error with an Error method that returns the string interpolation of the given
// format string and arguments.
func TemporaryErrorf(temporary bool, format string, a ...any) error {
	temp := struct{ temporaryProto }{}
	temp.temporaryMethod = func() bool { return temporary }
	temp.msgMethod = func() string { return fmt.Sprintf(format, a...) }
	temp.causeMethod = nil
	return temp
}

// TemporaryWrap wraps an existing error in both a temporary error and a causer error. This is done by creating an
// instance of the temporaryProto and assigning the appropriate methods.
func TemporaryWrap(temporary bool, err error, message string) error {
	temp := struct{ temporaryProto }{}
	temp.temporaryMethod = func() bool { return temporary }
	temp.msgMethod = func() string { return message }
	temp.causeMethod = func() error { return err }
	return temp
}

// TemporaryWrapf calls TemporaryWrap using the output of fmt.Sprintf as the message for the wrapped error.
func TemporaryWrapf(temporary bool, err error, format string, a ...any) error {
	return TemporaryWrap(temporary, err, fmt.Sprintf(format, a...))
}

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
