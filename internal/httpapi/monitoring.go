package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

func (s *Server) monitoringConfigured(w http.ResponseWriter) bool {
	if !s.Config.MonitoringEnabled || s.Monitoring == nil {
		problem(w, 503, "not_configured", "Les mesures du serveur ne sont pas activées.")
		return false
	}
	return true
}
func (s *Server) getMonitoring(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if !s.monitoringConfigured(w) {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "La vue du serveur ne reçoit aucun filtre personnel.")
		return
	}
	if s.limited(w, r, "monitoring", 30) {
		return
	}
	snapshot, err := s.Monitoring.Current(r.Context(), s.Config)
	if err != nil {
		problem(w, 503, "monitoring_unavailable", "Les mesures ne peuvent pas être actualisées.")
		return
	}
	if !s.revalidateUser(w, r, key, u) {
		return
	}
	jsonOut(w, 200, snapshot)
}
func (s *Server) getMonitoringHistory(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if !s.monitoringConfigured(w) {
		return
	}
	q := r.URL.Query()
	window := q.Get("range")
	if window == "" {
		window = "24h"
	}
	if len(q) > 1 || len(q["range"]) > 1 || (len(q) == 1 && len(q["range"]) == 0) || (window != "1h" && window != "24h" && window != "7d") {
		problem(w, 400, "invalid_query", "Choisissez une période : 1h, 24h ou 7d.")
		return
	}
	if s.limited(w, r, "monitoring-history", 12) {
		return
	}
	now := time.Now().UTC()
	items, step, err := s.Store.MetricHistory(r.Context(), now, window)
	if err != nil {
		problem(w, 500, "storage_error", "L’historique des mesures est indisponible.")
		return
	}
	if !s.revalidateUser(w, r, key, u) {
		return
	}
	jsonOut(w, 200, map[string]any{"items": items, "range": window, "stepSeconds": step, "aggregation": "mean", "retentionDays": 7, "updatedAt": now})
}
func (s *Server) adminPlexActivity(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if !s.Config.Admin(u.DiscordID) {
		problem(w, 403, "admin_required", "Les détails sont réservés aux administrateurs.")
		return
	}
	if !s.monitoringConfigured(w) {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Les détails ne reçoivent aucun filtre personnel.")
		return
	}
	if s.limited(w, r, "monitoring-admin", 30) {
		return
	}
	snapshot, err := s.Monitoring.Current(r.Context(), s.Config)
	if err != nil {
		problem(w, 503, "monitoring_unavailable", "L’activité ne peut pas être actualisée.")
		return
	}
	if !s.revalidateUser(w, r, key, u) {
		return
	}
	current, err := s.Store.User(r.Context(), u.ID)
	if err != nil || !s.Config.Admin(current.DiscordID) {
		problem(w, 403, "admin_required", "Accès administrateur requis.")
		return
	}
	jsonOut(w, 200, map[string]any{"updatedAt": snapshot.UpdatedAt, "expiresAt": snapshot.ExpiresAt, "sessions": snapshot.Sessions, "hostVersion": snapshot.HostVersion, "hostDetails": snapshot.HostDetails, "activity": snapshot.Activity})
}

func (s *Server) RunMonitoring(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.Store.PruneMetricSamples(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.Warn("Monitoring retention unavailable")
		}
		if s.Config.MonitoringEnabled && s.Monitoring != nil {
			if err := s.Monitoring.Sample(ctx, s.Config); err != nil && ctx.Err() == nil {
				slog.Warn("Monitoring sample unavailable")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
