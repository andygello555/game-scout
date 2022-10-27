package twitter

import (
	"fmt"
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
