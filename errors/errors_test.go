package errors

import (
	"fmt"
	"github.com/pkg/errors"
	"testing"
	"time"
)

func ExampleMergeErrors() {
	// Error messages will be merged using fmt.Errorf with semicolon separators
	fmt.Println(MergeErrors(
		errors.New("error 1"),
		errors.New("error 2"),
		errors.New("error 3"),
	))

	// Nil errors will be ignored
	fmt.Println(MergeErrors(
		errors.New("error 1"),
		nil,
		errors.New("error 2"),
	))

	// Merging no errors returns nil
	fmt.Println(MergeErrors())
	// Output:
	// error 1; error 2; error 3
	// error 1; error 2
	// <nil>
}

func TestMergeErrors(t *testing.T) {
	for i, test := range []struct {
		errs        []error
		expectNil   bool
		expectedMsg string
	}{
		{
			errs: []error{
				errors.New("error 1"),
				errors.New("error 2"),
				errors.New("error 3"),
			},
			expectedMsg: "error 1; error 2; error 3",
		},
		{
			errs: []error{
				errors.New("error 1"),
				nil,
				errors.New("error 2"),
			},
			expectedMsg: "error 1; error 2",
		},
		{
			errs:        []error{},
			expectedMsg: "",
			expectNil:   true,
		},
		{
			errs:        []error{nil, nil, nil},
			expectedMsg: "",
			expectNil:   true,
		},
	} {
		err := MergeErrors(test.errs...)
		if test.expectNil {
			if err != nil {
				t.Errorf("Expected merged error no. %d to be nil. It is: %v", i+1, err)
			}
		} else {
			if test.expectedMsg != err.Error() {
				t.Errorf(
					"Expected merged error no. %d to have the message \"%s\". Got \"%s\" instead",
					i+1, test.expectedMsg, err.Error(),
				)
			}
		}
	}
}

func TestRetry(t *testing.T) {
	for testNo, test := range []struct {
		maxTries    int
		args        []any
		retryFunc   RetryFunction
		expectedErr error
	}{
		{
			maxTries: 3,
			retryFunc: func(currentTry int, maxTries int, minDelay time.Duration, args ...any) error {
				return nil
			},
			expectedErr: nil,
		},
		{
			maxTries: 3,
			retryFunc: func(currentTry int, maxTries int, minDelay time.Duration, args ...any) error {
				return errors.Errorf("oh no error occurred in try no. %d", currentTry)
			},
			expectedErr: errors.New("ran out of tries (3 total) whilst calling retrier: oh no error occurred in try no. 3"),
		},
		{
			maxTries: 3,
			args:     []any{1, 2, 3},
			retryFunc: func(currentTry int, maxTries int, minDelay time.Duration, args ...any) error {
				return errors.Errorf("oh no error occurred with args: %v, on try no. %d", args, currentTry)
			},
			expectedErr: errors.New("ran out of tries (3 total) whilst calling retrier: oh no error occurred with args: [1 2 3], on try no. 3"),
		},
	} {
		err := Retry(test.maxTries, time.Second*0, test.retryFunc, test.args...)
		if test.expectedErr == nil {
			if err != nil {
				t.Errorf("Expected error no. %d to be nil. It is: \"%s\"", testNo+1, err.Error())
			}
		} else {
			if err == nil {
				t.Errorf(
					"Expected error no. %d to have the message \"%s\". It is nil",
					testNo+1, test.expectedErr.Error(),
				)
			} else if err.Error() != test.expectedErr.Error() {
				t.Errorf(
					"Expected error no. %d to have the message \"%s\". Got \"%s\" instead",
					testNo+1, test.expectedErr.Error(), err.Error(),
				)
			}
		}
	}
}
