package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type DownloadarrPin struct {
	URL       string    `json:"url"`
	FlowID    string    `json:"-"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type DownloadarrContract struct {
	ExpiresAt      time.Time `json:"expiresAt"`
	QualityOptions []string  `json:"qualityOptions"`
}
type DownloadInput struct {
	RatingKey string `json:"ratingKey"`
	Quality   string `json:"quality"`
	PartKey   string `json:"partKey,omitempty"`
	Season    bool   `json:"season,omitempty"`
	Audio     string `json:"audio,omitempty"`
	Subtitle  string `json:"subtitle,omitempty"`
}
type DownloadStreamOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type DownloadOptions struct {
	Audio     []DownloadStreamOption `json:"audio"`
	Subtitle  []DownloadStreamOption `json:"subtitle"`
	Season    bool                   `json:"season"`
	FileCount int                    `json:"fileCount"`
}
type DownloadJob struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	RatingKey     string    `json:"ratingKey"`
	CreatedAt     time.Time `json:"createdAt"`
	Season        bool      `json:"season"`
	Quality       string    `json:"quality"`
	Filename      string    `json:"filename"`
	State         string    `json:"state"`
	ReadyCount    int       `json:"readyCount"`
	FileCount     int       `json:"fileCount"`
	Stage         string    `json:"stage"`
	Progress      *float64  `json:"progress"`
	Error         string    `json:"error,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Reused        bool      `json:"reused,omitempty"`
	AudioLabel    string    `json:"audioLabel"`
	SubtitleLabel string    `json:"subtitleLabel"`
}

// Only fixed codes cross this boundary; provider text may contain private URLs.
type DownloadarrError struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (e *DownloadarrError) Error() string { return "downloadarr: " + e.Code }
func (e *DownloadarrError) Unwrap() error {
	if e.Status == 401 || e.Status == 403 {
		return ErrDenied
	}
	if e.Status == 404 || e.Status == 410 {
		return ErrNotFound
	}
	return ErrUnavailable
}

var downloadarrHex = regexp.MustCompile(`^[a-f0-9]{64}$`)
var downloadarrRevision = regexp.MustCompile(`^[a-f0-9]{40}$`)
var downloadarrID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
var downloadarrNumber = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var downloadarrSelection = regexp.MustCompile(`^(plex|none|stream:[1-9][0-9]{0,15}|match:[A-Za-z0-9_-]+)$`)

func validDownloadSelection(value string) bool {
	return len(value) <= 1506 && downloadarrSelection.MatchString(value)
}

func downloadarrInvalid() error    { return &DownloadarrError{Status: 502, Code: "invalid_response"} }
func downloadarrInputError() error { return &DownloadarrError{Status: 400, Code: "invalid_input"} }
func downloadarrQuality(value string) bool {
	return value == "720p-2" || value == "720p-4" || value == "1080p-8"
}
func validDownloadInput(input DownloadInput) bool {
	if !downloadarrNumber.MatchString(input.RatingKey) || !downloadarrQuality(input.Quality) {
		return false
	}
	if input.PartKey != "" && (len(input.PartKey) > 1024 || !strings.HasPrefix(input.PartKey, "/library/parts/") ||
		strings.ContainsAny(input.PartKey, "?#\\") || strings.IndexFunc(input.PartKey, unicode.IsControl) >= 0 || strings.Contains(input.PartKey, "..")) {
		return false
	}
	for _, value := range []string{input.Audio, input.Subtitle} {
		if value != "" && (!validDownloadSelection(value) ||
			(input.Season && strings.HasPrefix(value, "stream:")) || (!input.Season && strings.HasPrefix(value, "match:"))) {
			return false
		}
	}
	return input.Audio != "none" && (!input.Season || input.PartKey == "")
}

func downloadarrTarget(base, path string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", downloadarrInputError()
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawPath = ""
	return u.String(), nil
}
func downloadarrAuthorizedTarget(base, path, bearer string) (string, error) {
	if !downloadarrHex.MatchString(bearer) {
		return "", &DownloadarrError{Status: 401, Code: "unauthenticated"}
	}
	return downloadarrTarget(base, path)
}
func (c *Client) downloadarrClient(stream bool) *http.Client {
	client := http.Client{}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	client.Jar = nil
	client.Timeout = 6 * time.Second
	if stream {
		client.Timeout = 0
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}
func downloadarrFailure(resp *http.Response) error {
	code := "unavailable"
	switch resp.StatusCode {
	case 400, 422:
		code = "invalid_input"
	case 401:
		code = "unauthenticated"
	case 403:
		code = "denied"
	case 404:
		code = "not_found"
	case 409:
		code = "conflict"
	case 410:
		code = "expired"
	case 429:
		code = "rate_limited"
	}
	e := &DownloadarrError{Status: resp.StatusCode, Code: code}
	if resp.StatusCode == 429 || resp.StatusCode == 503 {
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds >= 1 && seconds <= 300 {
			e.RetryAfter = time.Duration(seconds) * time.Second
		}
	}
	return e
}
func downloadarrRequest(ctx context.Context, method, target, bearer string, body any) (*http.Request, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, downloadarrInputError()
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return nil, downloadarrInputError()
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		if !downloadarrHex.MatchString(bearer) {
			return nil, &DownloadarrError{Status: 401, Code: "unauthenticated"}
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return req, nil
}
func (c *Client) downloadarrJSON(ctx context.Context, method, target, bearer string, body, out any, statuses ...int) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := downloadarrRequest(ctx, method, target, bearer, body)
	if err != nil {
		return 0, err
	}
	resp, err := c.downloadarrClient(false).Do(req)
	if err != nil {
		return 0, &DownloadarrError{Status: 503, Code: "unavailable"}
	}
	defer resp.Body.Close()
	allowed := false
	for _, status := range statuses {
		allowed = allowed || resp.StatusCode == status
	}
	if !allowed {
		return resp.StatusCode, downloadarrFailure(resp)
	}
	if out == nil {
		return resp.StatusCode, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 || json.Unmarshal(data, out) != nil {
		return resp.StatusCode, downloadarrInvalid()
	}
	return resp.StatusCode, nil
}

func (c *Client) DownloadarrStart(ctx context.Context, base string) (DownloadarrPin, error) {
	started := time.Now().UTC()
	target, err := downloadarrTarget(base, "/api/auth/plex/pin")
	if err != nil {
		return DownloadarrPin{}, err
	}
	var raw struct{ URL, FlowID string }
	if _, err = c.downloadarrJSON(ctx, "POST", target, "", struct{}{}, &raw, 200); err != nil {
		return DownloadarrPin{}, err
	}
	u, err := url.Parse(raw.URL)
	if err != nil || len(raw.URL) > 4096 || u.Scheme != "https" || u.Host != "app.plex.tv" || u.Path != "/auth" ||
		u.User != nil || u.RawQuery != "" || !strings.HasPrefix(u.Fragment, "?") || !downloadarrHex.MatchString(raw.FlowID) {
		return DownloadarrPin{}, downloadarrInvalid()
	}
	query, err := url.ParseQuery(strings.TrimPrefix(u.Fragment, "?"))
	if err != nil || len(query) != 3 || len(query["clientID"]) != 1 || len(query["code"]) != 1 ||
		query.Get("clientID") == "" || query.Get("code") == "" || len(query["context[device][product]"]) != 1 ||
		query.Get("context[device][product]") != "LibraryDownloadarr" {
		return DownloadarrPin{}, downloadarrInvalid()
	}
	// The fork stores a five-minute deadline but does not return it; start locally
	// before the request so this conservative deadline never extends that flow.
	return DownloadarrPin{URL: raw.URL, FlowID: raw.FlowID, ExpiresAt: started.Add(5 * time.Minute)}, nil
}
func (c *Client) DownloadarrAuthenticate(ctx context.Context, base, flowID string) (string, bool, error) {
	if !downloadarrHex.MatchString(flowID) {
		return "", false, downloadarrInputError()
	}
	target, err := downloadarrTarget(base, "/api/auth/plex/authenticate")
	if err != nil {
		return "", false, err
	}
	var raw struct {
		Token   string `json:"token"`
		Pending bool   `json:"pending"`
	}
	status, err := c.downloadarrJSON(ctx, "POST", target, "", map[string]string{"flowId": flowID}, &raw, 200, 202)
	if err != nil {
		if status == 400 {
			err = &DownloadarrError{Status: 400, Code: "flow_expired"}
		}
		return "", false, err
	}
	if status == 202 && raw.Pending && raw.Token == "" {
		return "", true, nil
	}
	if status != 200 || raw.Pending || !downloadarrHex.MatchString(raw.Token) {
		return "", false, downloadarrInvalid()
	}
	return raw.Token, false, nil
}
func (c *Client) DownloadarrVerify(ctx context.Context, base, bearer, plexID, serverID, expectedRevision string) (DownloadarrContract, error) {
	if !downloadarrNumber.MatchString(plexID) || serverID == "" || len(serverID) > 256 || !downloadarrRevision.MatchString(expectedRevision) || bearer == "" {
		return DownloadarrContract{}, downloadarrInputError()
	}
	target, err := downloadarrAuthorizedTarget(base, "/api/integrations/concierge", bearer)
	if err != nil {
		return DownloadarrContract{}, err
	}
	var raw struct {
		ContractVersion  int                     `json:"contractVersion"`
		BuildRevision    string                  `json:"buildRevision"`
		User             struct{ PlexID string } `json:"user"`
		PlexServerID     string                  `json:"plexServerId"`
		SessionExpiresAt int64                   `json:"sessionExpiresAt"`
		Capabilities     struct {
			QualityOptions                                     []string `json:"qualityOptions"`
			StreamOptions, PreparedDownloads, SingleUseTickets bool
			QueuePersistence                                   string `json:"queuePersistence"`
		} `json:"capabilities"`
	}
	if _, err = c.downloadarrJSON(ctx, "GET", target, bearer, nil, &raw, 200); err != nil {
		return DownloadarrContract{}, err
	}
	if raw.User.PlexID != plexID || raw.PlexServerID != serverID {
		return DownloadarrContract{}, &DownloadarrError{Status: 403, Code: "identity_mismatch"}
	}
	expires := time.UnixMilli(raw.SessionExpiresAt).UTC()
	if !expires.After(time.Now()) || expires.After(time.Now().Add(24*time.Hour+time.Minute)) {
		return DownloadarrContract{}, &DownloadarrError{Status: 401, Code: "expired"}
	}
	cap := raw.Capabilities
	if raw.ContractVersion != 1 || raw.BuildRevision != expectedRevision || !cap.StreamOptions || !cap.PreparedDownloads || !cap.SingleUseTickets || cap.QueuePersistence != "memory" || len(cap.QualityOptions) == 0 || len(cap.QualityOptions) > 3 {
		return DownloadarrContract{}, &DownloadarrError{Status: 503, Code: "incompatible"}
	}
	seen := map[string]bool{}
	for _, quality := range cap.QualityOptions {
		if !downloadarrQuality(quality) || seen[quality] {
			return DownloadarrContract{}, downloadarrInvalid()
		}
		seen[quality] = true
	}
	return DownloadarrContract{ExpiresAt: expires, QualityOptions: cap.QualityOptions}, nil
}

func downloadarrText(value string, max int) string {
	if !utf8.ValidString(value) {
		return ""
	}
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value))
	if len(runes) > max {
		runes = runes[:max]
	}
	return strings.TrimSpace(string(runes))
}

type downloadarrJobWire struct {
	ID, Title, RatingKey, Quality, Filename, State, Stage, AudioLabel, SubtitleLabel string
	CreatedAt, ExpiresAt                                                             int64
	Season, Reused                                                                   bool
	ReadyCount, FileCount                                                            int
	Progress                                                                         json.RawMessage
}

func (raw downloadarrJobWire) normalized() (DownloadJob, error) {
	var progress *float64
	if !downloadarrID.MatchString(raw.ID) || !downloadarrNumber.MatchString(raw.RatingKey) || !downloadarrQuality(raw.Quality) ||
		raw.CreatedAt <= 0 || raw.ExpiresAt <= raw.CreatedAt || raw.FileCount < 1 || raw.FileCount > 100 ||
		raw.ReadyCount < 0 || raw.ReadyCount > raw.FileCount || (!raw.Season && raw.FileCount != 1) ||
		len(raw.Progress) == 0 || json.Unmarshal(raw.Progress, &progress) != nil || (progress != nil && (*progress < 0 || *progress > 100)) ||
		len(raw.Filename) > 512 || !utf8.ValidString(raw.Filename) || strings.ContainsAny(raw.Filename, "<>:\"/\\|?*") || strings.IndexFunc(raw.Filename, unicode.IsControl) >= 0 || raw.Filename == "" {
		return DownloadJob{}, downloadarrInvalid()
	}
	if raw.State != "preparing" && raw.State != "ready" && raw.State != "sending" && raw.State != "error" {
		return DownloadJob{}, downloadarrInvalid()
	}
	if raw.Stage != "deciding" && raw.Stage != "waiting" && raw.Stage != "processing" && raw.Stage != "finalizing" && raw.Stage != "ready" {
		return DownloadJob{}, downloadarrInvalid()
	}
	if (raw.State == "ready" || raw.State == "sending") && (raw.Stage != "ready" || progress == nil || *progress != 100 || raw.ReadyCount != raw.FileCount) {
		return DownloadJob{}, downloadarrInvalid()
	}
	if raw.State == "preparing" && (raw.Stage == "ready" || (progress != nil && *progress >= 100)) {
		return DownloadJob{}, downloadarrInvalid()
	}
	created, expires := time.UnixMilli(raw.CreatedAt).UTC(), time.UnixMilli(raw.ExpiresAt).UTC()
	if created.After(time.Now().Add(time.Minute)) || expires.After(time.Now().Add(24*time.Hour+time.Minute)) ||
		(raw.State != "sending" && !expires.After(time.Now())) {
		return DownloadJob{}, downloadarrInvalid()
	}
	title := downloadarrText(raw.Title, 512)
	if title == "" || (raw.Season && !strings.HasSuffix(raw.Filename, ".zip")) || (!raw.Season && !strings.HasSuffix(raw.Filename, ".mp4")) {
		return DownloadJob{}, downloadarrInvalid()
	}
	job := DownloadJob{ID: raw.ID, Title: title, RatingKey: raw.RatingKey, CreatedAt: created, Season: raw.Season,
		Quality: raw.Quality, Filename: raw.Filename, State: raw.State, ReadyCount: raw.ReadyCount, FileCount: raw.FileCount,
		Stage: raw.Stage, Progress: progress, ExpiresAt: expires, Reused: raw.Reused,
		AudioLabel: downloadarrText(raw.AudioLabel, 512), SubtitleLabel: downloadarrText(raw.SubtitleLabel, 512)}
	if raw.State == "error" {
		job.Error = "La préparation a échoué ou le fichier n’est plus disponible."
	}
	return job, nil
}

func (c *Client) DownloadarrJobs(ctx context.Context, base, bearer string) ([]DownloadJob, error) {
	target, err := downloadarrAuthorizedTarget(base, "/api/downloads", bearer)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Jobs []downloadarrJobWire `json:"jobs"`
	}
	if _, err = c.downloadarrJSON(ctx, "GET", target, bearer, nil, &raw, 200); err != nil {
		return nil, err
	}
	if raw.Jobs == nil || len(raw.Jobs) > 2 {
		return nil, downloadarrInvalid()
	}
	jobs := make([]DownloadJob, 0, len(raw.Jobs))
	seen := map[string]bool{}
	for _, wire := range raw.Jobs {
		job, err := wire.normalized()
		if err != nil {
			return nil, err
		}
		if seen[job.ID] {
			return nil, downloadarrInvalid()
		}
		seen[job.ID] = true
		jobs = append(jobs, job)
	}
	return jobs, nil
}
func (c *Client) DownloadarrOptions(ctx context.Context, base, bearer string, input DownloadInput) (DownloadOptions, error) {
	if !validDownloadInput(input) {
		return DownloadOptions{}, downloadarrInputError()
	}
	target, err := downloadarrAuthorizedTarget(base, "/api/downloads/options", bearer)
	if err != nil {
		return DownloadOptions{}, err
	}
	query := url.Values{"ratingKey": {input.RatingKey}, "quality": {input.Quality}, "season": {strconv.FormatBool(input.Season)}}
	if input.PartKey != "" {
		query.Set("partKey", input.PartKey)
	}
	var options DownloadOptions
	if _, err = c.downloadarrJSON(ctx, "GET", target+"?"+query.Encode(), bearer, nil, &options, 200); err != nil {
		return DownloadOptions{}, err
	}
	if options.Season != input.Season || options.FileCount < 1 || options.FileCount > 100 || (!options.Season && options.FileCount != 1) {
		return DownloadOptions{}, downloadarrInvalid()
	}
	for index, list := range [][]DownloadStreamOption{options.Audio, options.Subtitle} {
		if len(list) < 1 || len(list) > 256 {
			return DownloadOptions{}, downloadarrInvalid()
		}
		seen := map[string]bool{}
		for i := range list {
			choice := &list[i]
			if !validDownloadSelection(choice.ID) || seen[choice.ID] || (index == 0 && choice.ID == "none") ||
				(options.Season && strings.HasPrefix(choice.ID, "stream:")) || (!options.Season && strings.HasPrefix(choice.ID, "match:")) {
				return DownloadOptions{}, downloadarrInvalid()
			}
			seen[choice.ID] = true
			choice.Label = downloadarrText(choice.Label, 512)
			if choice.Label == "" {
				return DownloadOptions{}, downloadarrInvalid()
			}
		}
		if !seen["plex"] || (index == 1 && !seen["none"]) {
			return DownloadOptions{}, downloadarrInvalid()
		}
	}
	return options, nil
}
func (c *Client) DownloadarrCreate(ctx context.Context, base, bearer string, input DownloadInput) (DownloadJob, error) {
	if !validDownloadInput(input) {
		return DownloadJob{}, downloadarrInputError()
	}
	target, err := downloadarrAuthorizedTarget(base, "/api/downloads", bearer)
	if err != nil {
		return DownloadJob{}, err
	}
	var raw downloadarrJobWire
	if _, err = c.downloadarrJSON(ctx, "POST", target, bearer, input, &raw, 202); err != nil {
		return DownloadJob{}, err
	}
	job, err := raw.normalized()
	if err != nil {
		return DownloadJob{}, err
	}
	if job.RatingKey != input.RatingKey || job.Quality != input.Quality || job.Season != input.Season {
		return DownloadJob{}, downloadarrInvalid()
	}
	return job, nil
}
func (c *Client) DownloadarrCancel(ctx context.Context, base, bearer, id string) error {
	if !downloadarrID.MatchString(id) {
		return downloadarrInputError()
	}
	target, err := downloadarrAuthorizedTarget(base, "/api/downloads/"+id, bearer)
	if err != nil {
		return err
	}
	_, err = c.downloadarrJSON(ctx, "DELETE", target, bearer, nil, nil, 204)
	return err
}
func (c *Client) DownloadarrTicket(ctx context.Context, base, bearer, id string) (string, error) {
	if !downloadarrID.MatchString(id) {
		return "", downloadarrInputError()
	}
	target, err := downloadarrAuthorizedTarget(base, "/api/downloads/"+id+"/ticket", bearer)
	if err != nil {
		return "", err
	}
	var raw struct {
		Ticket string `json:"ticket"`
	}
	if _, err = c.downloadarrJSON(ctx, "POST", target, bearer, struct{}{}, &raw, 200); err != nil {
		return "", err
	}
	if !downloadarrHex.MatchString(raw.Ticket) {
		return "", downloadarrInvalid()
	}
	return raw.Ticket, nil
}

type downloadarrBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *downloadarrBody) Close() error { b.cancel(); return b.ReadCloser.Close() }

// DownloadarrFile uses the single-use ticket only, in a POST body. The caller
// owns the streaming deadline, closes Body and forwards only safe file headers.
func (c *Client) DownloadarrFile(ctx context.Context, base, id, ticket string) (*http.Response, error) {
	if !downloadarrID.MatchString(id) || !downloadarrHex.MatchString(ticket) {
		return nil, downloadarrInputError()
	}
	target, err := downloadarrTarget(base, "/api/downloads/"+id+"/file")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	req, err := downloadarrRequest(ctx, "POST", target, "", map[string]string{"ticket": ticket})
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "video/mp4, application/zip")
	// Bound opening the response, not the duration of a successfully opened file.
	timer := time.AfterFunc(15*time.Second, cancel)
	resp, err := c.downloadarrClient(true).Do(req)
	timer.Stop()
	if err != nil {
		cancel()
		return nil, &DownloadarrError{Status: 503, Code: "unavailable"}
	}
	mediaType, _, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != 200 || parseErr != nil || (mediaType != "video/mp4" && mediaType != "application/zip") {
		defer resp.Body.Close()
		cancel()
		if resp.StatusCode != 200 {
			return nil, downloadarrFailure(resp)
		}
		return nil, downloadarrInvalid()
	}
	resp.Body = &downloadarrBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}
