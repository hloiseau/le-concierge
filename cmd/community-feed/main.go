// community-feed validates or refreshes an explicitly approved publication.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/community"
)

func main() {
	path := flag.String("check", "", "validate a publication file")
	guild := flag.String("guild-id", "", "Discord guild ID")
	forums := flag.String("forum-ids", "", "comma-separated explicitly published forum IDs")
	issues := flag.String("issue-ids", "", "comma-separated selected owner/repository#number references")
	example := flag.Bool("example", false, "write a neutral one-hour example to stdout")
	syncManifest := flag.String("sync", "", "refresh explicitly selected upstream items from a manifest")
	output := flag.String("output", "", "publication artifact to replace atomically (required for -sync)")
	flag.Parse()
	if *syncManifest != "" {
		manifestPath, _ := filepath.Abs(*syncManifest)
		outputPath, _ := filepath.Abs(*output)
		if *output == "" || *path != "" || *example || flag.NArg() != 0 || strings.EqualFold(manifestPath, outputPath) || strings.EqualFold(manifestPath, outputPath+".retry-after") {
			fmt.Fprintln(os.Stderr, "Use -sync MANIFEST -output PUBLICATION with different paths")
			os.Exit(2)
		}
		m, err := community.ReadManifest(*syncManifest)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Invalid manifest; existing publication withdrawn")
			if removeErr := community.Withdraw(*output); removeErr != nil {
				fmt.Fprintln(os.Stderr, "Unable to withdraw publication")
			}
			os.Exit(1)
		}
		client := community.SyncClient{DiscordToken: os.Getenv("COMMUNITY_DISCORD_TOKEN"), GitHubToken: os.Getenv("COMMUNITY_GITHUB_TOKEN")}
		if err = client.SyncToFile(context.Background(), m, *output, time.Now().UTC()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			var upstream *community.SyncError
			if errors.As(err, &upstream) && upstream.RetryAfter > 0 {
				fmt.Fprintf(os.Stderr, "Do not retry for at least %.0f seconds\n", upstream.RetryAfter.Seconds())
			}
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "Selected community publication refreshed; valid for five minutes")
		return
	}
	if *example {
		now := time.Now().UTC().Truncate(time.Second)
		p := community.Publication{Version: 1, PublishedAt: now, ExpiresAt: now.Add(time.Hour), Forums: []community.PublishedThread{{ForumID: "11111", ThreadID: "22222", Title: "Exemple de proposition publiée", Kind: "proposal", Status: "open", UpdatedAt: now}}, Roadmap: []community.PublishedIssue{{IssueID: "example/project#1", ID: "example-feature", Title: "Exemple d’évolution", Status: "planned", Dependencies: []string{}, ThreadID: "22222"}}}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(p); err != nil {
			fmt.Fprintln(os.Stderr, "Unable to write example")
			os.Exit(1)
		}
		return
	}
	if *path == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Use -example, -sync MANIFEST -output PUBLICATION, or -check FILE with -guild-id, -forum-ids and/or -issue-ids")
		os.Exit(2)
	}
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(s, ",")
	}
	snapshot, err := community.Load(*path, community.Selection{GuildID: *guild, ForumIDs: split(*forums), IssueIDs: split(*issues)}, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Valid publication: %d threads, %d roadmap items; expires %s\n", len(snapshot.Threads), len(snapshot.Milestones), snapshot.ExpiresAt.Format(time.RFC3339))
}
