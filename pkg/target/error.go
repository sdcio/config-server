package target

// TransactionError represents an error with recoverability classification
type TransactionError struct {
	Recoverable bool
	Err     error
}

func (e *TransactionError) Error() string {
	return e.Err.Error()
}

// NewTransactionError creates a new TransactionError
func NewTransactionError(err error, recoverable bool) error {
	return &TransactionError{
		Recoverable: recoverable,
		Err:     err,
	}
}