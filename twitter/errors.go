package twitter

import "fmt"

// temporary indicates whether an error is temporary
type temporary interface {
	Temporary() bool
}

// temporaryProto acts as a prototype for anonymous structs to implement the temporary interface.
type temporaryProto struct {
	temporaryMethod func() bool
	errorMethod     func() string
}

// Temporary calls the temporaryMethod of the prototype, thus implementing the temporary interface.
func (tp temporaryProto) Temporary() bool {
	return tp.temporaryMethod()
}

// Error calls the errorMethod of the prototype, thus implementing the error interface.
func (tp temporaryProto) Error() string {
	return tp.errorMethod()
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
	temp.errorMethod = func() string { return fmt.Sprintf(format, a...) }
	return temp
}
