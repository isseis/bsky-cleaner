package config

import (
	"errors"
	"testing"
)

func setAllCredentialEnv(t *testing.T, handle, appPassword, slackSuccessURL, slackFailureURL string) {
	t.Helper()
	t.Setenv("BSKY_HANDLE", handle)
	t.Setenv("BSKY_APP_PASSWORD", appPassword)
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", slackSuccessURL)
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_FAILURE", slackFailureURL)
}

func TestLoadCredentials_Success(t *testing.T) {
	setAllCredentialEnv(t, "alice.bsky.social", "app-password", "https://hooks.slack.com/services/success", "https://hooks.slack.com/services/failure")

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() unexpected error: %v", err)
	}

	if creds.Handle != "alice.bsky.social" {
		t.Errorf("Handle = %q, want %q", creds.Handle, "alice.bsky.social")
	}
	if creds.AppPassword.Reveal() != "app-password" {
		t.Errorf("AppPassword.Reveal() = %q, want %q", creds.AppPassword.Reveal(), "app-password")
	}
	if creds.SlackSuccessWebhookURL.Reveal() != "https://hooks.slack.com/services/success" {
		t.Errorf("SlackSuccessWebhookURL.Reveal() = %q, want %q", creds.SlackSuccessWebhookURL.Reveal(), "https://hooks.slack.com/services/success")
	}
	if creds.SlackFailureWebhookURL.Reveal() != "https://hooks.slack.com/services/failure" {
		t.Errorf("SlackFailureWebhookURL.Reveal() = %q, want %q", creds.SlackFailureWebhookURL.Reveal(), "https://hooks.slack.com/services/failure")
	}
}

func TestLoadCredentials_MissingRequiredEnv(t *testing.T) {
	tests := []struct {
		name  string
		field string
	}{
		{name: "handle missing", field: "BSKY_HANDLE"},
		{name: "app password missing", field: "BSKY_APP_PASSWORD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BSKY_HANDLE", "alice.bsky.social")
			t.Setenv("BSKY_APP_PASSWORD", "app-password")
			unsetEnv(t, tt.field)

			_, err := LoadCredentials()
			if !errors.Is(err, ErrMissingEnv) {
				t.Fatalf("LoadCredentials() error = %v, want wrapping ErrMissingEnv", err)
			}

			fieldErr, ok := errors.AsType[*FieldError](err)
			if !ok {
				t.Fatalf("LoadCredentials() error = %v, want *FieldError", err)
			}
			if fieldErr.Field != tt.field {
				t.Errorf("FieldError.Field = %q, want %q", fieldErr.Field, tt.field)
			}
			if fieldErr.Value != "" {
				t.Errorf("FieldError.Value = %q, want empty string", fieldErr.Value)
			}
		})
	}
}

func TestLoadCredentials_SlackWebhookURLOptional(t *testing.T) {
	t.Setenv("BSKY_HANDLE", "alice.bsky.social")
	t.Setenv("BSKY_APP_PASSWORD", "app-password")
	unsetEnv(t, "BSKY_SLACK_WEBHOOK_URL_SUCCESS")
	unsetEnv(t, "BSKY_SLACK_WEBHOOK_URL_FAILURE")

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() unexpected error: %v", err)
	}

	if creds.SlackSuccessWebhookURL != (SecretString{}) {
		t.Errorf("SlackSuccessWebhookURL = %#v, want zero value", creds.SlackSuccessWebhookURL)
	}
	if creds.SlackFailureWebhookURL != (SecretString{}) {
		t.Errorf("SlackFailureWebhookURL = %#v, want zero value", creds.SlackFailureWebhookURL)
	}
}

func TestLoadCredentials_SlackWebhookURLInvalidScheme(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "http scheme", url: "http://hooks.slack.com/services/success"},
		{name: "syntactically invalid", url: "://not-a-url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BSKY_HANDLE", "alice.bsky.social")
			t.Setenv("BSKY_APP_PASSWORD", "app-password")
			t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", tt.url)
			unsetEnv(t, "BSKY_SLACK_WEBHOOK_URL_FAILURE")

			_, err := LoadCredentials()
			if !errors.Is(err, ErrInvalidValue) {
				t.Fatalf("LoadCredentials() error = %v, want wrapping ErrInvalidValue", err)
			}

			fieldErr, ok := errors.AsType[*FieldError](err)
			if !ok {
				t.Fatalf("LoadCredentials() error = %v, want *FieldError", err)
			}
			if fieldErr.Field != "BSKY_SLACK_WEBHOOK_URL_SUCCESS" {
				t.Errorf("FieldError.Field = %q, want %q", fieldErr.Field, "BSKY_SLACK_WEBHOOK_URL_SUCCESS")
			}
			if fieldErr.Value != "" {
				t.Errorf("FieldError.Value = %q, want empty string", fieldErr.Value)
			}
		})
	}
}

func TestLoadCredentials_SlackWebhookURLOnlyOneSet(t *testing.T) {
	t.Setenv("BSKY_HANDLE", "alice.bsky.social")
	t.Setenv("BSKY_APP_PASSWORD", "app-password")
	t.Setenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS", "https://hooks.slack.com/services/success")
	unsetEnv(t, "BSKY_SLACK_WEBHOOK_URL_FAILURE")

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() unexpected error: %v", err)
	}

	if creds.SlackSuccessWebhookURL.Reveal() != "https://hooks.slack.com/services/success" {
		t.Errorf("SlackSuccessWebhookURL.Reveal() = %q, want %q", creds.SlackSuccessWebhookURL.Reveal(), "https://hooks.slack.com/services/success")
	}
	if creds.SlackFailureWebhookURL != (SecretString{}) {
		t.Errorf("SlackFailureWebhookURL = %#v, want zero value", creds.SlackFailureWebhookURL)
	}
}
