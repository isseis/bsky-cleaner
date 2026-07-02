package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFieldError_ErrorAndUnwrap(t *testing.T) {
	fieldErr := &FieldError{Field: "retention_days", Value: "0", Err: ErrInvalidValue}

	assert.Contains(t, fieldErr.Error(), "retention_days")
	assert.ErrorIs(t, fieldErr, ErrInvalidValue)
}
