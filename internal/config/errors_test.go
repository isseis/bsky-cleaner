//go:build test

package config

import (
	"errors"
	"strings"
	"testing"
)

func TestFieldError_ErrorAndUnwrap(t *testing.T) {
	fieldErr := &FieldError{Field: "retention_days", Value: "0", Err: ErrInvalidValue}

	if !strings.Contains(fieldErr.Error(), "retention_days") {
		t.Errorf("Error() = %q, want it to contain the field name", fieldErr.Error())
	}
	if !errors.Is(fieldErr, ErrInvalidValue) {
		t.Errorf("errors.Is(fieldErr, ErrInvalidValue) = false, want true")
	}
}
