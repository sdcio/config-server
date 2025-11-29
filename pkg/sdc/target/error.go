package target

import (
	errors "errors"
	"fmt"
)

// TransactionError represents an error with recoverability classification
type TransactionError struct {
	Recoverable bool
	Err         error
}

func (e *TransactionError) Error() string {
	return e.Err.Error()
}

// NewTransactionError creates a new TransactionError
func NewTransactionError(err error, recoverable bool) error {
	return &TransactionError{
		Recoverable: recoverable,
		Err:         err,
	}
}

var ErrLookup = errors.New("target lookup error")

var TargetLookupErr *LookupError

type LookupError struct {
	Message      string
	WrappedError error
}

func (e *LookupError) Error() string {
	if e.WrappedError != nil {
		return fmt.Sprintf("%s, err: %s", e.Message, e.WrappedError.Error())
	}
	return e.Message
}

func (e *LookupError) Unwrap() error {
	return e.WrappedError
}