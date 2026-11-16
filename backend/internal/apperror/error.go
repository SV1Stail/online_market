package apperror

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound         = errors.New("resource not found")
	ErrUnauthenticated  = errors.New("authentication required")
	ErrPermissionDenied = errors.New("permission denied")
	ErrConflict         = errors.New("resource version conflict")
	ErrPrecondition     = errors.New("precondition failed")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func Validation(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
