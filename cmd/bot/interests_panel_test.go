package main

import (
	"flag"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func panelRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	previousArgs, previousFlags, previousOutput := os.Args, flag.CommandLine, os.Stdout
	defer func() { os.Args, flag.CommandLine, os.Stdout = previousArgs, previousFlags, previousOutput }()
	os.Args = append([]string{"bot"}, args...)
	flag.CommandLine = flag.NewFlagSet("bot", flag.ContinueOnError)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	err = run()
	writer.Close()
	output, readErr := io.ReadAll(reader)
	reader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(output), err
}

func TestInterestPanelFlagsAreExplicitAndExclusive(t *testing.T) {
	for _, args := range [][]string{
		{"--register", "--publish-interests"},
		{"--message-id", "67890"},
		{"--register", "--message-id", "67890"},
		{"--message-id="},
		{"--publish-interests", "--message-id="},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			output, err := panelRun(t, args...)
			if err == nil || output != "" || !strings.Contains(err.Error(), "--publish-interests") {
				t.Fatalf("invalid flag combination: %q %v", output, err)
			}
		})
	}
}

func TestInterestPanelCommandPrintsOnlyTheVerifiedID(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("DISCORD_CLIENT_ID", "12345")
	t.Setenv("DISCORD_GUILD_ID", "23456")
	t.Setenv("DISCORD_BOT_TOKEN", "private-panel-token")
	t.Setenv("DISCORD_BOT_TOKEN_FILE", "")
	t.Setenv("DISCORD_PUBLIC_KEY", "")
	t.Setenv("DISCORD_CLIENT_SECRET", "")
	t.Setenv("DISCORD_INTERESTS_CHANNEL_ID", "34567")
	t.Setenv("DISCORD_INTERESTS_JSON", `[{"key":"films","label":"Films","roleId":"45678","channelId":"56789"}]`)
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	for _, update := range []bool{false, true} {
		writes := 0
		http.DefaultTransport = registrationTransport(func(r *http.Request) (*http.Response, error) {
			body := `{"id":"67890","channel_id":"34567","author":{"id":"12345"}}`
			if r.Method == http.MethodGet && r.URL.Path == "/api/v10/users/@me" {
				body = `{"id":"12345","bot":true}`
			}
			if r.Method == http.MethodGet && r.URL.Path == "/api/v10/channels/34567" {
				body = `{"id":"34567","guild_id":"23456","type":0}`
			}
			if r.Method != http.MethodGet {
				writes++
				if (update && r.Method != http.MethodPatch) || (!update && r.Method != http.MethodPost) {
					t.Fatal("wrong publication mode")
				}
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		args := []string{"--publish-interests"}
		if update {
			args = append(args, "--message-id", "67890")
		}
		output, err := panelRun(t, args...)
		if err != nil || output != "Interest panel message: 67890\n" || writes != 1 {
			t.Fatalf("panel command: %q %v, writes=%d", output, err, writes)
		}
	}
}
