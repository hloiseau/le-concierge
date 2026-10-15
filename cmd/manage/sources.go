package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

// Diagnostics expose counts only, never history, titles, identities or secrets.
func checkSources(ctx context.Context, c config.Config, client *upstream.Client, plexUser string, out io.Writer) error {
	if c.MaintainerrURL == "" && c.TautulliURL == "" {
		return fmt.Errorf("configure MAINTAINERR_URL or TAUTULLI_URL first")
	}
	if plexUser != "" && c.TautulliURL == "" {
		return fmt.Errorf("--plex-user requires Tautulli")
	}
	result := map[string]any{"remindersEnabled": false}
	if c.MaintainerrURL != "" {
		snapshot, err := client.MaintainerrRemovals(ctx, c.MaintainerrURL, c.PlexServerID)
		if err != nil {
			return fmt.Errorf("Maintainerr: %w", err)
		}
		unknown := 0
		for _, item := range snapshot.Items {
			if item.EstimatedAt == nil {
				unknown++
			}
		}
		result["maintainerr"] = map[string]any{"version": snapshot.Version, "build": snapshot.Build, "serverMatches": true, "collections": snapshot.Collections, "destructiveMemberships": len(snapshot.Items), "unknownDeadlines": unknown, "updatedAt": snapshot.UpdatedAt}
	}
	if c.TautulliURL != "" {
		var version string
		var err error
		status := map[string]any{"serverMatches": true}
		if plexUser == "" {
			version, err = client.TautulliStatus(ctx, c.TautulliURL, c.TautulliAPIKey, c.PlexServerID)
		} else {
			var history upstream.History
			history, err = client.TautulliHistory(ctx, c.TautulliURL, c.TautulliAPIKey, c.PlexServerID, plexUser)
			version = history.Version
			status["recordedPlays"] = len(history.Items)
			status["updatedAt"] = history.UpdatedAt
		}
		if err != nil {
			return fmt.Errorf("Tautulli: %w", err)
		}
		status["version"] = version
		result["tautulli"] = status
	}
	return json.NewEncoder(out).Encode(result)
}
