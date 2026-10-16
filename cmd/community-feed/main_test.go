package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/community"
)

func TestCommunityCLIProcess(t *testing.T) {
	if os.Getenv("COMMUNITY_FEED_CLI_TEST") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
			break
		}
	}
	flag.CommandLine = flag.NewFlagSet("community-feed", flag.ExitOnError)
	main()
	os.Exit(0)
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=TestCommunityCLIProcess", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "COMMUNITY_FEED_CLI_TEST=1", "COMMUNITY_DISCORD_TOKEN=", "COMMUNITY_GITHUB_TOKEN=")
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func TestCLIExampleCheckAndEmptySyncWithoutUpstream(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "publication.json")
	b, err := runCLI(t, "-example")
	if err != nil {
		t.Fatalf("example %v %s", err, b)
	}
	var p community.Publication
	if json.Unmarshal([]byte(b), &p) != nil || len(p.Forums) != 1 || len(p.Roadmap) != 1 {
		t.Fatal("invalid CLI example")
	}
	if err = os.WriteFile(output, []byte(b), 0600); err != nil {
		t.Fatal(err)
	}
	if b, err = runCLI(t, "-check", output, "-guild-id", "33333", "-forum-ids", "11111", "-issue-ids", "example/project#1"); err != nil || !strings.Contains(b, "Valid publication") {
		t.Fatalf("check %v %s", err, b)
	}
	manifest := filepath.Join(dir, "manifest.json")
	if err = os.WriteFile(manifest, []byte(`{"version":1,"forums":[],"issues":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if b, err = runCLI(t, "-sync", manifest, "-output", output); err != nil {
		t.Fatalf("sync %v %s", err, b)
	}
	snapshot, err := community.Load(output, community.Selection{}, time.Now())
	if err != nil || len(snapshot.Threads) != 0 || len(snapshot.Milestones) != 0 || snapshot.ExpiresAt.Sub(snapshot.PublishedAt) != 5*time.Minute {
		t.Fatalf("empty sync did not replace publication %+v %v", snapshot, err)
	}
	if err = os.WriteFile(manifest, []byte(`{"invalid":"DO NOT LOG PRIVATE TEXT"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if b, err = runCLI(t, "-sync", manifest, "-output", output); err == nil || strings.Contains(b, "DO NOT LOG") {
		t.Fatalf("invalid manifest accepted or exposed %v %s", err, b)
	}
	if _, err = os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid manifest retained old publication")
	}
}

func TestCLIRejectsPublicationOverwritingManifestOrCooldown(t *testing.T) {
	for _, suffix := range []string{"", ".retry-after"} {
		output := filepath.Join(t.TempDir(), "publication.json")
		manifest := output + suffix
		body := []byte(`{"version":1,"forums":[],"issues":[]}`)
		if err := os.WriteFile(manifest, body, 0600); err != nil {
			t.Fatal(err)
		}
		if b, err := runCLI(t, "-sync", manifest, "-output", output); err == nil {
			t.Fatalf("overlap accepted: %s", b)
		}
		if after, err := os.ReadFile(manifest); err != nil || string(after) != string(body) {
			t.Fatal("manifest was changed")
		}
	}
}
