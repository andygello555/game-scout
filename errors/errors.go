package errors

import (
	"fmt"
	"github.com/pkg/errors"
	"time"
)

type stackTracer interface {
	StackTrace() errors.StackTrace
}

// temporary indicates whether an error is temporary
type temporary interface {
	Temporary() bool
}

// temporaryProto acts as a prototype for anonymous structs to implement the temporary interface.
type temporaryProto struct {
	temporaryMethod func() bool
	msgMethod       func() string
	causeMethod     func() error
	// causedItself is set when the error returned by causeMethod is the temporaryProto itself wrapped as a stackTracer.
	causedItself bool
}

// Temporary calls the temporaryMethod of the prototype, thus implementing the temporary interface.
func (tp temporaryProto) Temporary() bool {
	return tp.temporaryMethod()
}

// Error returns the outcome of the msgMethod of the prototype if causedItself is set. Otherwise, the result of
// msgMethod will be concatenated with causeMethod.
func (tp temporaryProto) Error() string {
	if tp.causedItself {
		return tp.msgMethod()
	}
	return tp.msgMethod() + ": " + tp.causeMethod().Error()
}

// Cause calls the causeMethod of the prototype, thus implementing the causer interface.
func (tp temporaryProto) Cause() error {
	return tp.causeMethod()
}

// StackTrace returns the errors.StackTrace of the Cause error.
func (tp temporaryProto) StackTrace() errors.StackTrace {
	return tp.causeMethod().(stackTracer).StackTrace()
}

// IsTemporary returns true if err is temporary.
func IsTemporary(err error) bool {
	te, ok := err.(temporary)
	return ok && te.Temporary()
}

// TemporaryError creates a new temporary error with an Error method that returns the given message.
func TemporaryError(temporary bool, msg string) error {
	return TemporaryErrorf(temporary, msg)
}

// TemporaryErrorf creates a new temporary error with an Error method that returns the string interpolation of the given
// format string and arguments.
func TemporaryErrorf(temporary bool, format string, a ...any) error {
	temp := struct{ temporaryProto }{}
	temp.temporaryMethod = func() bool { return temporary }
	temp.msgMethod = func() string { return fmt.Sprintf(format, a...) }
	// Make the temporary error aware by adding a stack
	tempWithStack := errors.WithStack(temp)
	temp.causeMethod = func() error { return tempWithStack }
	temp.causedItself = true
	return temp
}

// TemporaryWrap wraps an existing error in both a temporary error and a causer error. This is done by creating an
// instance of the temporaryProto and assigning the appropriate methods.
func TemporaryWrap(temporary bool, err error, message string) error {
	temp := struct{ temporaryProto }{}
	temp.temporaryMethod = func() bool { return temporary }
	temp.msgMethod = func() string { return message }

	// Make the error stack traceable if it is not already
	if _, ok := err.(stackTracer); !ok {
		err = errors.WithStack(err)
	}

	temp.causeMethod = func() error { return err }
	temp.causedItself = false
	return temp
}

// TemporaryWrapf calls TemporaryWrap using the output of fmt.Sprintf as the message for the wrapped error.
func TemporaryWrapf(temporary bool, err error, format string, a ...any) error {
	return TemporaryWrap(temporary, err, fmt.Sprintf(format, a...))
}

// MergeErrors merges all the given errors into one error. This is done by using the fmt.Errorf function and separating
// each error with a semicolon. If an error is nil, then the error won't be merged. If there are no non-nil errors given
// (this includes providing 0 errors) then nil will be returned. The returned error implements stackTracer.
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
	return errors.WithStack(mergedErr)
}

// RetryFunction is the signature of the function that the Retry function must be given.
type RetryFunction func(currentTry int, maxTries int, minDelay time.Duration, args ...any) error

// RetryReturnType are the return types that are used within the Retry function.
type RetryReturnType int

const (
	// Continue to the next try.
	Continue RetryReturnType = iota
	// Break out of try loop. This also applies to Break-ing out after all tries have been exhausted because there is a
	// recurring error.
	Break
	// Done with the try loop. Exit the try loop gracefully without there being a recurring error.
	Done
)

func (rrt RetryReturnType) String() string {
	switch rrt {
	case Continue:
		return "continue"
	case Break:
		return "break"
	case Done:
		return "done"
	default:
		return "unknown"
	}
}

// Error returns the error message for this RetryReturnType.
func (rrt RetryReturnType) Error() string {
	return fmt.Sprintf("retry function wants to \"%s\"", rrt.String())
}

// Retry will retry the given function the given number of times. The function will be retried only if it returns an
// error (that is not a RetryReturnType error) or a panic occurs within it. If the error is a RetryReturnType then the
// following actions will be carried out, depending on which RetryReturnType is returned:
//
//   - Continue: will proceed to the next try of the RetryFunction without waiting. The output error for Retry is also
//     set to nil, faking the success scenario. Note: this will still use up a try. This is to avoid infinite loops.
//
//   - Break: will stop retrying and will set Break as the returned error for Retry. I.e. fakes the failure scenario.
//
//   - Done: will stop retrying and will set nil as the returned error. This is the same as returning nil from the
//     RetryFunction. I.e. fakes the success scenario.
//
// If after the given number of maxTries, an error is still occurring in the tried function, the error will be returned.
// If the RetryFunction is nil, then an appropriate error will be returned.
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
				// If the error returned is a RetryReturnType then we will perform the actions described above.
				// Note: we use errors.As here because errors returned from the RetryFunction might be wrapped.
				if errors.As(err, &rt) {
					switch rt {
					case Continue:
						// If we are on the last try then we will set the RetryReturnType to Break.
						if tries > 0 {
							tries--
						} else {
							rt = Break
						}
						fallthrough
					case Done:
						// Only Continue and Done will activate the "success" scenario where err is nilled.
						err = nil
					}
					return rt
				} else {
					if tries > 0 {
						tries--
						time.Sleep(minDelay * time.Duration(maxTries+1-tries))
						return Continue
					}
					err = errors.Wrapf(err, "ran out of tries (%d total) whilst calling retrier", maxTries)
					return Break
				}
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
