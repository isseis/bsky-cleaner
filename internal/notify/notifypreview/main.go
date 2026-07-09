//go:build test

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
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
		fmt.Printf("=== %s ===\n%s\n", s.name, notify.BuildPayloadPreview(s.outcome))
	}
}

// sendScenarios posts each scenario via the production notify.Send, using
// the single test webhook URL from BSKY_SLACK_WEBHOOK_URL_TEST for both the
// success and failure destinations.
func sendScenarios(scenarios []scenario) error {
	url := os.Getenv(testWebhookEnvVar)
	if url == "" {
		return fmt.Errorf("%s is not set; create a test-channel Incoming Webhook and set it before using -send", testWebhookEnvVar)
	}
	webhook := config.NewSecretStringForTest(url)
	cfg := notify.Config{SuccessWebhookURL: webhook, FailureWebhookURL: webhook}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, s := range scenarios {
		fmt.Printf("sending %q...\n", s.name)
		if err := notify.Send(ctx, cfg, http.DefaultClient, retry.RealClock{}, s.outcome); err != nil {
			return fmt.Errorf("scenario %q: %w", s.name, err)
		}
	}
	return nil
}
