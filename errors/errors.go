package errors

import (
	"fmt"
	"github.com/pkg/errors"
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

// MergeErrors merges all the given errors into one error. This is done by using the fmt.Errorf function and separating
// each error with a semicolon. If an error is nil, then the error won't be merged. If there are no non-nil errors given
// (this includes providing 0 errors) then nil will be returned.
func MergeErrors(errs ...error) error {
	var mergedErr error
	for _, err := range errs {
		if err != nil {
			if mergedErr == nil {
				mergedErr = err
			} else {
				mergedErr = fmt.Errorf("%w; %s", mergedErr, err.Error())
			}
		}
	}
	return mergedErr
}

// RetryFunction is the signature of the function that the Retry function must be given.
type RetryFunction func(currentTry int, maxTries int, minDelay time.Duration, args ...any) error

// RetryReturnType are the return types that are used within the Retry function.
type RetryReturnType int

const (
	Continue RetryReturnType = iota
	Break
	Done
)

// Retry will retry the given function the given number of times. The function will be retried only if it returns an
// error or a panic occurs within it. If after the given number of maxTries, an error is still occurring in the tried
// function, the error will be returned. If the RetryFunction is nil, then an appropriate error will be returned.
func Retry(maxTries int, minDelay time.Duration, retry RetryFunction, args ...any) (err error) {
	if retry == nil {
		return errors.New("retry function cannot be nil")
	}

	tries := maxTries
	for {
		rt := func() (rt RetryReturnType) {
			defer func() {
				if pan := recover(); pan != nil {
					if tries > 0 {
						tries--
						time.Sleep(minDelay * time.Duration(maxTries+1-tries))
						rt = Continue
						return
					}
					err = fmt.Errorf("panic occurred: %v", pan)
					rt = Break
				}
			}()

			if err = retry(maxTries-tries, maxTries, minDelay, args...); err != nil {
				if tries > 0 {
					tries--
					time.Sleep(minDelay * time.Duration(maxTries+1-tries))
					return Continue
				}
				err = errors.Wrapf(err, "ran out of tries (%d total) whilst calling retrier", maxTries)
				return Break
			}
			return Done
		}()

		if rt == Continue {
			continue
		}
		break
	}
	return
}
