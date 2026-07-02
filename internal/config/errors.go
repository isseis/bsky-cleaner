package config

import (
	"errors"
	"fmt"
)

// Sentinel errors identifying the category of a configuration load
// failure. Callers use errors.Is to check for these regardless of the
// specific field involved.
var (
	ErrFileNotFound = errors.New("config file not found")
	ErrParseFailed  = errors.New("config file parse failed")
	ErrMissingField = errors.New("required field is missing")
	ErrInvalidValue = errors.New("field value is invalid")
	ErrMissingEnv   = errors.New("required environment variable is missing")
)

// FieldError identifies which configuration field caused a validation
// failure, wrapping one of the sentinel errors above so callers can use
// errors.Is / errors.AsType[*FieldError] instead of matching on the error
// message string. Value holds the offending raw value for non-secret
// (TOML-derived) fields only; it is left empty for credential-derived
// errors, since those fields hold secret values.
type FieldError struct {
	Field string
	Value string
	Err   error
}

func (e *FieldError) Error() string {
	if e.Value == "" {
		return fmt.Sprintf("field %q: %v", e.Field, e.Err)
	}
	return fmt.Sprintf("field %q (value %q): %v", e.Field, e.Value, e.Err)
}

func (e *FieldError) Unwrap() error {
	return e.Err
}
