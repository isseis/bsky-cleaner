//go:build test

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/isseis/bsky-cleaner/internal/config"
	"github.com/isseis/bsky-cleaner/internal/notify"
	"github.com/isseis/bsky-cleaner/internal/retry"
)

// testWebhookEnvVar is the single test-channel destination used for both
// the success and failure webhook slots when -send is given: unlike
// production (which routes to two separate channels), a preview run always
// targets the one channel the developer pointed at BSKY_SLACK_WEBHOOK_URL_TEST.
const testWebhookEnvVar = "BSKY_SLACK_WEBHOOK_URL_TEST"

func main() {
	scenarioFlag := flag.String("scenario", "", "name of the scenario to render (default: all)")
	send := flag.Bool("send", false, "POST the rendered payload to "+testWebhookEnvVar+" instead of printing it")
	flag.Parse()

	all := scenarios()
	selected, err := selectScenarios(all, *scenarioFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *send {
		if err := sendScenarios(selected); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	printScenarios(selected)
}

// selectScenarios returns all if name is empty, otherwise the single
// scenario matching name.
func selectScenarios(all []scenario, name string) ([]scenario, error) {
	if name == "" {
		return all, nil
	}
	idx := slices.IndexFunc(all, func(s scenario) bool { return s.name == name })
	if idx < 0 {
		names := make([]string, len(all))
		for i, s := range all {
			names[i] = s.name
		}
		return nil, fmt.Errorf("unknown scenario %q; available: %v", name, names)
	}
	return all[idx : idx+1], nil
}

// printScenarios renders each scenario via the production formatter
// (notify.BuildPayloadPreview) and prints it to stdout.
func printScenarios(scenarios []scenario) {
	for _, s := range scenarios {
		p := notify.BuildPayloadPreview(s.outcome)
		fmt.Printf("=== %s ===\n", s.name)
		fmt.Printf("  Text: %s\n", p.Text)
		for i, a := range p.Attachments {
			fmt.Printf("  Attachment[%d]:\n", i)
			fmt.Printf("    Color: %s\n", a.Color)
			for _, f := range a.Fields {
				fmt.Printf("    Field: %s = %s\n", f.Title, f.Value)
			}
		}
	}
}

// sendScenarios posts each scenario via the production notify.Send, using
// the single test webhook URL from BSKY_SLACK_WEBHOOK_URL_TEST for both the
// success and failure destinations.
func sendScenarios(scenarios []scenario) error {
	rawURL := os.Getenv(testWebhookEnvVar)
	if rawURL == "" {
		return fmt.Errorf("%s is not set; create a test-channel Incoming Webhook and set it before using -send", testWebhookEnvVar)
	}
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return fmt.Errorf("invalid %s value %q: %w", testWebhookEnvVar, rawURL, err)
	}
	webhook := config.NewSecretStringForTest(rawURL)
	cfg := notify.Config{SuccessWebhookURL: webhook, FailureWebhookURL: webhook}

	for _, s := range scenarios {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		fmt.Printf("sending %q...\n", s.name)
		if err := notify.Send(ctx, cfg, http.DefaultClient, retry.RealClock{}, s.outcome); err != nil {
			cancel()
			return fmt.Errorf("scenario %q: %w", s.name, err)
		}
		cancel()
	}
	return nil
}
