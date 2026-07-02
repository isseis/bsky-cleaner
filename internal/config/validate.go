package config

import (
	"net/url"
	"strconv"
	"time"
)

// maxExecutionTimeoutSeconds is the upper bound for execution_timeout_seconds
// (24 hours). It is checked before the seconds-to-nanoseconds conversion so
// that an excessively large configured value cannot overflow time.Duration
// (an int64 count of nanoseconds) and wrap around to an unintended small or
// negative duration.
const maxExecutionTimeoutSeconds = 24 * 60 * 60

// validateConfig checks raw for missing required fields and out-of-range
// values, returning the validated Config only when every field passes.
func validateConfig(raw rawConfig) (Config, error) {
	switch {
	case raw.RetentionDays == nil:
		return Config{}, &FieldError{Field: "retention_days", Err: ErrMissingField}
	case raw.Schedule == nil:
		return Config{}, &FieldError{Field: "schedule", Err: ErrMissingField}
	case raw.ExecutionTimeoutSeconds == nil:
		return Config{}, &FieldError{Field: "execution_timeout_seconds", Err: ErrMissingField}
	}

	if *raw.RetentionDays <= 0 {
		return Config{}, &FieldError{
			Field: "retention_days",
			Value: strconv.Itoa(*raw.RetentionDays),
			Err:   ErrInvalidValue,
		}
	}

	timeoutSeconds := *raw.ExecutionTimeoutSeconds
	if timeoutSeconds <= 0 || timeoutSeconds > maxExecutionTimeoutSeconds {
		return Config{}, &FieldError{
			Field: "execution_timeout_seconds",
			Value: strconv.Itoa(timeoutSeconds),
			Err:   ErrInvalidValue,
		}
	}

	return Config{
		RetentionDays:    *raw.RetentionDays,
		Schedule:         *raw.Schedule,
		ExecutionTimeout: time.Duration(timeoutSeconds) * time.Second,
	}, nil
}

// validateCredentials checks the required fields for presence and the
// optional Slack webhook URLs for well-formedness, returning the validated
// Credentials only when every field passes. slackSuccessURL and
// slackFailureURL may be empty, meaning "not configured".
func validateCredentials(handle, appPassword, slackSuccessURL, slackFailureURL string) (Credentials, error) {
	if handle == "" {
		return Credentials{}, &FieldError{Field: "BSKY_HANDLE", Err: ErrMissingEnv}
	}
	if appPassword == "" {
		return Credentials{}, &FieldError{Field: "BSKY_APP_PASSWORD", Err: ErrMissingEnv}
	}

	if err := validateWebhookURL("BSKY_SLACK_WEBHOOK_URL_SUCCESS", slackSuccessURL); err != nil {
		return Credentials{}, err
	}
	if err := validateWebhookURL("BSKY_SLACK_WEBHOOK_URL_FAILURE", slackFailureURL); err != nil {
		return Credentials{}, err
	}

	return Credentials{
		Handle:                 handle,
		AppPassword:            SecretString{value: appPassword},
		SlackSuccessWebhookURL: SecretString{value: slackSuccessURL},
		SlackFailureWebhookURL: SecretString{value: slackFailureURL},
	}, nil
}

// validateWebhookURL checks that a Slack webhook URL, if set, is a
// syntactically valid https URL. An empty rawURL (not configured) is not an
// error.
func validateWebhookURL(field, rawURL string) error {
	if rawURL == "" {
		return nil
	}

	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return &FieldError{Field: field, Err: ErrInvalidValue}
	}
	return nil
}
