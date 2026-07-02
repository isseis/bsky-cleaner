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

// LoadCredentials reads secret configuration values from the process
// environment (BSKY_HANDLE, BSKY_APP_PASSWORD, BSKY_SLACK_WEBHOOK_URL_SUCCESS,
// BSKY_SLACK_WEBHOOK_URL_FAILURE via os.Getenv) and validates them via
// validateCredentials. It takes no parameters: it only ever reads these
// four fixed variable names, never an arbitrary key.
func LoadCredentials() (*Credentials, error) {
	handle := os.Getenv("BSKY_HANDLE")
	appPassword := os.Getenv("BSKY_APP_PASSWORD")
	slackSuccessURL := os.Getenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS")
	slackFailureURL := os.Getenv("BSKY_SLACK_WEBHOOK_URL_FAILURE")

	creds, err := validateCredentials(handle, appPassword, slackSuccessURL, slackFailureURL)
	if err != nil {
		return nil, err
	}
	return &creds, nil
}
