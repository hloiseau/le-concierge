package upstream

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type DiagnosticSources struct {
	SeerrURL, SeerrKey, RadarrURL, RadarrKey, SonarrURL, SonarrKey string
	PlexURL, PlexServerID                                          string
}
type DiagnosticEvidence struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	At      string `json:"at,omitempty"`
}
type Diagnostic struct {
	Source           string               `json:"source"`
	Version          string               `json:"version"`
	Stage            string               `json:"stage"`
	Summary          string               `json:"summary"`
	Evidence         []DiagnosticEvidence `json:"evidence"`
	CheckedAt        time.Time            `json:"checkedAt"`
	QueueTruncated   bool                 `json:"queueTruncated"`
	HistoryTruncated bool                 `json:"historyTruncated"`
	PlexStatus       string               `json:"plexStatus"`
	GuideURL         string               `json:"guideUrl,omitempty"`
}
type diagnosticRequest struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	Is4K      bool      `json:"is4k"`
	ServerID  *int64    `json:"serverId"`
	Media     struct {
		TMDBID      int64  `json:"tmdbId"`
		TVDBID      int64  `json:"tvdbId"`
		Type        string `json:"mediaType"`
		RatingKey   string `json:"ratingKey"`
		RatingKey4K string `json:"ratingKey4k"`
	} `json:"media"`
	Seasons []struct {
		Number int `json:"seasonNumber"`
	} `json:"seasons"`
}
type diagnosticArrItem struct {
	ID      int64 `json:"id"`
	TMDBID  int64 `json:"tmdbId"`
	TVDBID  int64 `json:"tvdbId"`
	HasFile bool  `json:"hasFile"`
	Seasons []struct {
		Number     int `json:"seasonNumber"`
		Statistics struct {
			Files int `json:"episodeFileCount"`
		} `json:"statistics"`
	} `json:"seasons"`
}
type diagnosticQueueItem struct {
	MovieID  int64  `json:"movieId"`
	SeriesID int64  `json:"seriesId"`
	Season   *int   `json:"seasonNumber"`
	State    string `json:"trackedDownloadState"`
	Error    string `json:"errorMessage"`
	Messages []struct {
		Messages []string `json:"messages"`
	} `json:"statusMessages"`
}
type diagnosticHistoryItem struct {
	MovieID  int64     `json:"movieId"`
	SeriesID int64     `json:"seriesId"`
	Event    string    `json:"eventType"`
	Date     time.Time `json:"date"`
	Episode  *struct {
		Season int `json:"seasonNumber"`
	} `json:"episode"`
}

var diagnosticVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,4}(?:[-+][A-Za-z0-9.-]+)?$`)

const manualImportGuide = "https://wiki.servarr.com/radarr/faq"

// DiagnoseRequest reads only the requested media, with no title-based correlation or raw log output.
func (c *Client) DiagnoseRequest(ctx context.Context, sources DiagnosticSources, grant Grant, requestID int64) (Diagnostic, error) {
	out := Diagnostic{Stage: "unknown", Summary: "Aucune cause confirmée dans les éléments consultés.", Evidence: []DiagnosticEvidence{}, CheckedAt: time.Now().UTC(), PlexStatus: "not_verified"}
	if requestID < 1 || requestID > 2147483647 || sources.SeerrURL == "" || sources.SeerrKey == "" {
		return out, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var request diagnosticRequest
	if err := c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/request/%d", sources.SeerrURL, requestID), map[string]string{"X-Api-Key": sources.SeerrKey}, "", &request); err != nil {
		return out, err
	}
	if request.ID != requestID || request.Media.TMDBID < 1 || (request.Type != "movie" && request.Type != "tv") || (request.Media.Type != "" && request.Media.Type != request.Type) || request.CreatedAt.IsZero() {
		return out, ErrIncomplete
	}
	base, key, lookup, filter := "", "", "", ""
	if request.Type == "movie" {
		out.Source = "radarr"
		base, key = sources.RadarrURL, sources.RadarrKey
		lookup = "movie?tmdbId=" + strconv.FormatInt(request.Media.TMDBID, 10)
		filter = "movieIds"
	} else {
		if request.Media.TVDBID < 1 || len(request.Seasons) == 0 {
			return out, ErrIncomplete
		}
		out.Source = "sonarr"
		base, key = sources.SonarrURL, sources.SonarrKey
		lookup = "series?tvdbId=" + strconv.FormatInt(request.Media.TVDBID, 10)
		filter = "seriesIds"
	}
	if base == "" || key == "" {
		return out, ErrUnavailable
	}
	if err := c.diagnosticSourceMatches(ctx, sources, request, out.Source, base); err != nil {
		return out, err
	}
	headers := map[string]string{"X-Api-Key": key}
	var status struct {
		Name    string `json:"appName"`
		Version string `json:"version"`
	}
	if err := c.JSON(ctx, "GET", base+"/api/v3/system/status", headers, "", &status); err != nil {
		return out, err
	}
	if !strings.EqualFold(status.Name, out.Source) || len(status.Version) > 50 || !diagnosticVersion.MatchString(status.Version) {
		return out, ErrIncomplete
	}
	out.Version = status.Version
	if request.Status == 1 {
		out.Stage = "approval"
		out.Summary = "La demande attend une approbation dans Seerr."
		return out, nil
	}
	if request.Status == 3 {
		out.Stage = "declined"
		out.Summary = "La demande est refusée dans Seerr."
		return out, nil
	}
	if request.Status != 2 && request.Status != 4 && request.Status != 5 {
		return out, ErrIncomplete
	}
	var matches []diagnosticArrItem
	if err := c.JSON(ctx, "GET", base+"/api/v3/"+lookup, headers, "", &matches); err != nil {
		return out, err
	}
	if len(matches) == 0 {
		out.Stage = "not_found"
		out.Summary = "Le média demandé n’est pas présent dans l’instance configurée. Vérifiez le routage de cette demande dans Seerr, notamment l’instance 4K."
		return out, nil
	}
	if len(matches) != 1 {
		return out, ErrIncomplete
	}
	item := matches[0]
	if item.ID <= 0 || (request.Type == "movie" && item.TMDBID != request.Media.TMDBID) || (request.Type == "tv" && (item.TVDBID != request.Media.TVDBID || (item.TMDBID != 0 && item.TMDBID != request.Media.TMDBID))) {
		return out, ErrIncomplete
	}
	seasons := map[int]bool{}
	for _, season := range request.Seasons {
		if season.Number < 0 || season.Number > 10000 {
			return out, ErrIncomplete
		}
		seasons[season.Number] = true
	}
	q := url.Values{filter: {strconv.FormatInt(item.ID, 10)}, "page": {"1"}, "pageSize": {"50"}}
	var queue struct {
		Page     int                   `json:"page"`
		PageSize int                   `json:"pageSize"`
		Total    *int                  `json:"totalRecords"`
		Records  []diagnosticQueueItem `json:"records"`
	}
	if err := c.JSON(ctx, "GET", base+"/api/v3/queue?"+q.Encode(), headers, "", &queue); err != nil {
		return out, err
	}
	if queue.Page != 1 || queue.PageSize != 50 || queue.Total == nil || *queue.Total < len(queue.Records) || len(queue.Records) > 50 {
		return out, ErrIncomplete
	}
	out.QueueTruncated = *queue.Total > len(queue.Records)
	priority := 0
	add := func(stage, message, at string, weight int) {
		if weight > priority {
			out.Stage = stage
			out.Summary = message
			priority = weight
		}
		for _, existing := range out.Evidence {
			if existing.Stage == stage && existing.Message == message && existing.At == at {
				return
			}
		}
		if len(out.Evidence) < 12 {
			out.Evidence = append(out.Evidence, DiagnosticEvidence{Stage: stage, Message: message, At: at})
		}
	}
	if request.Status == 4 {
		add("unknown", "Seerr signale un échec de traitement de cette demande. Vérifiez aussi son envoi vers l’application configurée.", "", 1)
	}
	for _, record := range queue.Records {
		if (request.Type == "movie" && record.MovieID != item.ID) || (request.Type == "tv" && record.SeriesID != item.ID) {
			return out, ErrIncomplete
		}
		if request.Type == "tv" && (record.Season == nil || !seasons[*record.Season]) {
			continue
		}
		messages := []string{record.Error}
		for _, group := range record.Messages {
			messages = append(messages, group.Messages...)
		}
		for _, message := range messages {
			if stage, safe, weight := diagnosticError(message); stage != "" {
				add(stage, safe, "", weight)
			}
		}
		switch record.State {
		case "downloading":
			add("downloading", "Téléchargement en cours dans le client relié.", "", 30)
		case "importPending", "importing":
			add("importing", "Téléchargement terminé ; import ou renommage en cours de traitement.", "", 40)
		case "importBlocked":
			add("importing", "L’import est bloqué. Consultez les détails dans l’application administrateur.", "", 50)
		case "failed", "failedPending":
			add("download_failed", "Le téléchargement a échoué ou attend le traitement de son échec.", "", 60)
		case "imported":
			add("imported", "L’application signale un import terminé pour un élément de cette demande.", "", 10)
		}
	}
	q.Set("pageSize", "30")
	q.Set("sortKey", "date")
	q.Set("sortDirection", "descending")
	if request.Type == "tv" {
		q.Set("includeEpisode", "true")
	}
	var history struct {
		Page     int                     `json:"page"`
		PageSize int                     `json:"pageSize"`
		Total    *int                    `json:"totalRecords"`
		Records  []diagnosticHistoryItem `json:"records"`
	}
	if err := c.JSON(ctx, "GET", base+"/api/v3/history?"+q.Encode(), headers, "", &history); err != nil {
		return out, err
	}
	if history.Page != 1 || history.PageSize != 30 || history.Total == nil || *history.Total < len(history.Records) || len(history.Records) > 30 {
		return out, ErrIncomplete
	}
	out.HistoryTruncated = *history.Total > len(history.Records)
	for _, record := range history.Records {
		if (request.Type == "movie" && record.MovieID != item.ID) || (request.Type == "tv" && record.SeriesID != item.ID) {
			return out, ErrIncomplete
		}
		if record.Date.IsZero() || record.Date.Before(request.CreatedAt) || (request.Type == "tv" && (record.Episode == nil || !seasons[record.Episode.Season])) {
			continue
		}
		at := record.Date.UTC().Format(time.RFC3339)
		switch record.Event {
		case "downloadFolderImported", "movieFolderImported", "seriesFolderImported":
			add("imported", "Historique : un import a été enregistré depuis la création de la demande.", at, 5)
		case "movieFileRenamed", "episodeFileRenamed":
			add("rename_import", "Historique : un renommage a été enregistré.", at, 0)
		case "downloadFailed":
			add("download_failed", "Historique : un échec de téléchargement a été enregistré ; il peut avoir été résolu depuis.", at, 0)
		case "grabbed":
			add("downloading", "Historique : un téléchargement a été envoyé au client.", at, 0)
		}
	}
	hasFile := request.Type == "movie" && item.HasFile
	if request.Type == "tv" {
		for _, season := range item.Seasons {
			hasFile = hasFile || (seasons[season.Number] && season.Statistics.Files > 0)
		}
	}
	if hasFile {
		add("plex_indexing", "Un fichier est répertorié pour ce média ou une saison demandée. Vérifiez l’indexation Plex ; cela ne prouve pas que toute la demande est disponible.", "", 15)
	}
	if priority >= 90 {
		out.GuideURL = manualImportGuide
	}
	ratingKey := request.Media.RatingKey
	if request.Is4K {
		ratingKey = request.Media.RatingKey4K
	}
	if ratingKey != "" && sources.PlexURL != "" && sources.PlexServerID != "" && grant.ServerToken != "" {
		if err := c.diagnosticPlexIndexed(ctx, sources, grant, ratingKey, request.Type, request.Media.TMDBID); err == nil {
			out.PlexStatus = "indexed"
			add("imported", "Les métadonnées du média sont indexées dans Plex et accessibles à votre compte administrateur. La lecture et la complétude des saisons ne sont pas vérifiées.", "", 16)
		}
	}
	return out, nil
}

// Only recognized invariant error clauses are exposed; free text may embed tracker names, paths or tokens.
func diagnosticError(raw string) (string, string, int) {
	message := strings.ToLower(raw)
	switch {
	case strings.Contains(message, "matched to movie by id") && strings.Contains(message, "manual import"):
		return "manual_import", "Found matching movie via grab history, but release was matched to movie by ID. Manual Import required.", 90
	case strings.Contains(message, "manual import required"):
		return "manual_import", "Manual Import required. Vérifiez le média avant toute intervention manuelle.", 85
	case strings.Contains(message, "not enough free space"), strings.Contains(message, "insufficient disk space"), strings.Contains(message, "no space left"):
		return "space", "Not enough free space — espace disque insuffisant pour l’import.", 80
	case strings.Contains(message, "permission denied"), strings.Contains(message, "access to the path") && strings.Contains(message, "denied"), strings.Contains(message, "unauthorizedaccessexception"):
		return "permissions", "Permission denied — accès au fichier ou au dossier refusé.", 80
	case strings.Contains(message, "does not exist or is not accessible"), strings.Contains(message, "path does not exist"):
		return "permissions", "Le dossier d’import est absent ou inaccessible. Vérifiez les montages et les droits.", 75
	case strings.Contains(message, "unable to rename"), strings.Contains(message, "failed to rename"), strings.Contains(message, "unable to move"), strings.Contains(message, "failed to move"):
		return "rename_import", "Échec de déplacement ou de renommage pendant l’import.", 70
	}
	if strings.TrimSpace(raw) != "" {
		return "importing", "Une erreur est signalée dans la file. Les détails libres restent dans l’application pour protéger chemins et identifiants.", 45
	}
	return "", "", 0
}
func (c *Client) diagnosticPlexIndexed(ctx context.Context, sources DiagnosticSources, grant Grant, key, kind string, tmdbID int64) error {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != key {
		return ErrIncomplete
	}
	if err := c.PlexIdentity(ctx, sources.PlexURL, sources.PlexServerID); err != nil {
		return err
	}
	items, _, err := c.subscriptionPlexPage(ctx, sources.PlexURL, "/library/metadata/"+key, grant, 0, 1)
	if err != nil {
		return err
	}
	if len(items) != 1 {
		return ErrNotFound
	}
	expected := kind
	if kind == "tv" {
		expected = "show"
	}
	if string(items[0].ID) != key || items[0].Kind != expected {
		return ErrIncomplete
	}
	for _, guid := range items[0].GUIDs {
		if guid.ID == "tmdb://"+strconv.FormatInt(tmdbID, 10) {
			return nil
		}
	}
	return ErrIncomplete
}

func (c *Client) diagnosticSourceMatches(ctx context.Context, sources DiagnosticSources, request diagnosticRequest, kind, base string) error {
	var servers []struct {
		ID       int64  `json:"id"`
		Hostname string `json:"hostname"`
		Port     int    `json:"port"`
		SSL      bool   `json:"useSsl"`
		BaseURL  string `json:"baseUrl"`
		Is4K     bool   `json:"is4k"`
		Default  bool   `json:"isDefault"`
	}
	if err := c.JSON(ctx, "GET", sources.SeerrURL+"/api/v1/settings/"+kind, map[string]string{"X-Api-Key": sources.SeerrKey}, "", &servers); err != nil {
		return err
	}
	if len(servers) > 100 {
		return ErrIncomplete
	}
	configured, err := url.Parse(base)
	if err != nil {
		return ErrIncomplete
	}
	matched := 0
	for _, server := range servers {
		selected := request.ServerID != nil && *request.ServerID == server.ID
		if request.ServerID == nil {
			selected = server.Default && server.Is4K == request.Is4K
		}
		if !selected {
			continue
		}
		matched++
		if server.Port < 1 || server.Port > 65535 || strings.ContainsAny(server.Hostname, "/@?#\\") {
			return ErrIncomplete
		}
		scheme := "http"
		if server.SSL {
			scheme = "https"
		}
		expected := &url.URL{Scheme: scheme, Host: net.JoinHostPort(strings.Trim(server.Hostname, "[]"), strconv.Itoa(server.Port)), Path: server.BaseURL}
		port := configured.Port()
		if port == "" {
			port = "80"
			if configured.Scheme == "https" {
				port = "443"
			}
		}
		if configured.Scheme != expected.Scheme || !strings.EqualFold(configured.Hostname(), expected.Hostname()) || port != expected.Port() || strings.Trim(configured.Path, "/") != strings.Trim(expected.Path, "/") {
			return ErrIncomplete
		}
	}
	if matched != 1 {
		return ErrIncomplete
	}
	return nil
}
