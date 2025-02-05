package target

// TransactionError represents an error with recoverability classification
type TransactionError struct {
	Recoverable bool
	Message     string
}

func (e *TransactionError) Error() string {
	return e.Message
}

// NewTransactionError creates a new TransactionError
func NewTransactionError(message string, recoverable bool) error {
	return &TransactionError{
		Recoverable: recoverable,
		Message:     message,
	}
}