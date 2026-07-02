package config

import "os"

// Credentials holds the secret configuration values read from the
// process environment.
type Credentials struct {
	Handle                 string
	AppPassword            SecretString
	SlackSuccessWebhookURL SecretString
	SlackFailureWebhookURL SecretString
}

// LoadCredentials reads and validates secret configuration values from
// the process environment: BSKY_HANDLE and BSKY_APP_PASSWORD are required
// and read via os.LookupEnv, while BSKY_SLACK_WEBHOOK_URL_SUCCESS and
// BSKY_SLACK_WEBHOOK_URL_FAILURE are optional and read via os.Getenv,
// defaulting to the empty string when unset. It takes no parameters: it
// only ever reads these four fixed variable names, never an arbitrary key.
func LoadCredentials() (*Credentials, error) {
	handle, ok := os.LookupEnv("BSKY_HANDLE")
	if !ok {
		return nil, &FieldError{Field: "BSKY_HANDLE", Err: ErrMissingEnv}
	}
	appPassword, ok := os.LookupEnv("BSKY_APP_PASSWORD")
	if !ok {
		return nil, &FieldError{Field: "BSKY_APP_PASSWORD", Err: ErrMissingEnv}
	}

	slackSuccessURL := os.Getenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS")
	slackFailureURL := os.Getenv("BSKY_SLACK_WEBHOOK_URL_FAILURE")

	return &Credentials{
		Handle:                 handle,
		AppPassword:            SecretString{value: appPassword},
		SlackSuccessWebhookURL: SecretString{value: slackSuccessURL},
		SlackFailureWebhookURL: SecretString{value: slackFailureURL},
	}, nil
}
