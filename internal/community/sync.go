package community

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Manifest struct {
	Version int              `json:"version"`
	GuildID string           `json:"guildId,omitempty"`
	Forums  []ForumSelection `json:"forums"`
	Issues  []IssueSelection `json:"issues"`
}
type ForumSelection struct {
	ID            string            `json:"id"`
	Kind          string            `json:"kind"`
	ThreadIDs     []string          `json:"threadIds"`
	StatusTags    map[string]string `json:"statusTags"`
	DefaultStatus string            `json:"defaultStatus,omitempty"`
}
type IssueSelection struct {
	IssueID         string            `json:"issueId"`
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	Summary         string            `json:"summary,omitempty"`
	Dependencies    []string          `json:"dependencies"`
	Date            string            `json:"date,omitempty"`
	DateConfirmed   bool              `json:"dateConfirmed,omitempty"`
	ThreadID        string            `json:"threadId,omitempty"`
	LinkToIssue     bool              `json:"linkToIssue,omitempty"`
	OpenStatus      string            `json:"openStatus,omitempty"`
	CompletedStatus string            `json:"completedStatus,omitempty"`
	LabelStatuses   map[string]string `json:"labelStatuses,omitempty"`
}

func ReadManifest(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, errors.New("sync manifest unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return Manifest{}, errors.New("invalid sync manifest file")
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil || len(b) > MaxBytes {
		return Manifest{}, errors.New("sync manifest too large")
	}
	var m Manifest
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || d.Decode(new(any)) != io.EOF {
		return Manifest{}, errors.New("invalid sync manifest JSON")
	}
	return m, m.Validate()
}

func (m Manifest) Validate() error {
	invalid := errors.New("invalid sync manifest selection")
	if m.Version != 1 || len(m.Forums) > 20 || len(m.Issues) > 50 || (len(m.Forums) > 0 && !ValidForumID(m.GuildID)) {
		return invalid
	}
	forums, threads, issues, ids := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, f := range m.Forums {
		if !ValidForumID(f.ID) || forums[f.ID] || !oneOf(f.Kind, "proposal", "problem") || len(f.StatusTags) > 20 || (f.DefaultStatus != "" && !oneOf(f.DefaultStatus, "open", "planned", "in_progress", "resolved", "declined")) {
			return invalid
		}
		forums[f.ID] = true
		for _, id := range f.ThreadIDs {
			if !ValidForumID(id) || threads[id] {
				return invalid
			}
			threads[id] = true
		}
		for id, status := range f.StatusTags {
			if !ValidForumID(id) || !oneOf(status, "open", "planned", "in_progress", "resolved", "declined") {
				return invalid
			}
		}
	}
	if len(threads) > 50 {
		return invalid
	}
	for _, item := range m.Issues {
		key := strings.ToLower(item.IssueID)
		if !ValidIssueID(item.IssueID) || issues[key] || !publicID.MatchString(item.ID) || ids[item.ID] || !plain(item.Title, 200, false) || !plain(item.Summary, 1000, true) || len(item.Dependencies) > 50 || len(item.LabelStatuses) > 20 {
			return invalid
		}
		issues[key], ids[item.ID] = true, true
		if item.ThreadID != "" && !threads[item.ThreadID] {
			return invalid
		}
		for _, status := range []string{item.OpenStatus, item.CompletedStatus} {
			if status != "" && !oneOf(status, "planned", "in_progress", "delivered") {
				return invalid
			}
		}
		for label, status := range item.LabelStatuses {
			if !plain(label, 100, false) || !oneOf(status, "planned", "in_progress", "delivered") {
				return invalid
			}
		}
		if item.DateConfirmed {
			if date, err := time.Parse("2006-01-02", item.Date); err != nil || date.Format("2006-01-02") != item.Date {
				return invalid
			}
		}
	}
	for _, item := range m.Issues {
		for _, id := range item.Dependencies {
			if !ids[id] || id == item.ID {
				return invalid
			}
		}
	}
	return nil
}

type SyncClient struct {
	HTTP         *http.Client
	DiscordToken string
	GitHubToken  string
}

type SyncError struct{ RetryAfter time.Duration }

func (e *SyncError) Error() string {
	return "community upstream unavailable or rate limited; publication withdrawn"
}

func (c SyncClient) read(ctx context.Context, provider, path string, out any) (bool, error) {
	base, auth := "https://api.github.com", ""
	if provider == "discord" {
		base = "https://discord.com/api/v10"
		auth = "Bot " + c.DiscordToken
	} else if c.GitHubToken != "" {
		auth = "Bearer " + c.GitHubToken
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return false, &SyncError{}
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "le-concierge-community-exporter")
	if provider == "github" {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	client := http.Client{Timeout: 5 * time.Second}
	if c.HTTP != nil {
		client = *c.HTTP
		client.Timeout = 5 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return false, &SyncError{}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && (resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0")) {
		delay := time.Minute
		if seconds, err := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); err == nil && seconds > 0 && seconds <= 86400 {
			delay = time.Duration(seconds * float64(time.Second))
		}
		if provider == "github" {
			if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && time.Until(time.Unix(reset, 0)) > delay {
				delay = time.Until(time.Unix(reset, 0))
			}
		}
		if provider == "discord" {
			var limit struct {
				RetryAfter float64 `json:"retry_after"`
			}
			if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&limit) == nil && limit.RetryAfter > delay.Seconds() && limit.RetryAfter <= 86400 {
				delay = time.Duration(limit.RetryAfter * float64(time.Second))
			}
		}
		return false, &SyncError{RetryAfter: delay}
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, &SyncError{}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil || len(body) > 1024*1024 || json.Unmarshal(body, out) != nil {
		return false, &SyncError{}
	}
	return true, nil
}

type discordOverwrite struct {
	ID    string `json:"id"`
	Type  int    `json:"type"`
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}
type discordChannel struct {
	ID            string              `json:"id"`
	GuildID       string              `json:"guild_id"`
	ParentID      string              `json:"parent_id"`
	Type          int                 `json:"type"`
	Name          string              `json:"name"`
	NSFW          *bool               `json:"nsfw"`
	Flags         uint64              `json:"flags"`
	Overwrites    *[]discordOverwrite `json:"permission_overwrites"`
	Tags          []string            `json:"applied_tags"`
	AvailableTags []struct {
		ID string `json:"id"`
	} `json:"available_tags"`
	ThreadMetadata *struct {
		Archived bool `json:"archived"`
	} `json:"thread_metadata"`
}

const publicReadPermissions uint64 = 1<<10 | 1<<16

func publicForum(ch discordChannel, guild string, everyone uint64) bool {
	if ch.Type != 15 || ch.GuildID != guild || ch.NSFW == nil || *ch.NSFW || ch.Flags&(1<<17|1<<21) != 0 || ch.Overwrites == nil {
		return false
	}
	permissions := everyone
	seen := map[string]bool{}
	for _, o := range *ch.Overwrites {
		allow, e1 := strconv.ParseUint(o.Allow, 10, 64)
		deny, e2 := strconv.ParseUint(o.Deny, 10, 64)
		if e1 != nil || e2 != nil || !ValidForumID(o.ID) || (o.Type != 0 && o.Type != 1) || seen[strconv.Itoa(o.Type)+":"+o.ID] {
			return false
		}
		seen[strconv.Itoa(o.Type)+":"+o.ID] = true
		if deny&publicReadPermissions != 0 {
			return false
		}
		if o.ID == guild {
			if o.Type != 0 {
				return false
			}
			permissions = (permissions &^ deny) | allow
		}
	}
	return permissions&publicReadPermissions == publicReadPermissions
}

func publishedThread(thread, parent discordChannel, selected ForumSelection, guild string, now time.Time) (PublishedThread, bool) {
	if thread.GuildID != guild || thread.ParentID != selected.ID || thread.Type != 11 || thread.ThreadMetadata == nil || (thread.NSFW != nil && *thread.NSFW) || thread.Flags&(1<<17|1<<21) != 0 || !plain(thread.Name, 200, false) {
		return PublishedThread{}, false
	}
	if thread.Overwrites != nil && len(*thread.Overwrites) > 0 {
		return PublishedThread{}, false
	}
	available := map[string]bool{}
	for _, tag := range parent.AvailableTags {
		available[tag.ID] = true
	}
	status, mapped := selected.DefaultStatus, ""
	for _, tag := range thread.Tags {
		if s := selected.StatusTags[tag]; s != "" {
			if !available[tag] || (mapped != "" && mapped != s) {
				return PublishedThread{}, false
			}
			mapped = s
		}
	}
	if mapped != "" {
		status = mapped
	}
	if status == "" {
		return PublishedThread{}, false
	}
	return PublishedThread{ForumID: selected.ID, ThreadID: thread.ID, Title: thread.Name, Kind: selected.Kind, Status: status, Archived: thread.ThreadMetadata.Archived, UpdatedAt: now.UTC()}, true
}

// Sync performs only selected GETs; no guild-wide thread or issue listing.
func (c SyncClient) Sync(ctx context.Context, m Manifest, now time.Time) (Publication, error) {
	if err := m.Validate(); err != nil {
		return Publication{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	p := Publication{Version: 1, PublishedAt: now.UTC(), ExpiresAt: now.UTC().Add(5 * time.Minute), Forums: []PublishedThread{}, Roadmap: []PublishedIssue{}}
	selection := Selection{GuildID: m.GuildID}
	if len(m.Forums) > 0 {
		if c.DiscordToken == "" {
			return Publication{}, errors.New("Discord read token required for selected forums")
		}
		var roles []struct {
			ID          string `json:"id"`
			Permissions string `json:"permissions"`
		}
		exists, err := c.read(ctx, "discord", "/guilds/"+m.GuildID+"/roles", &roles)
		if err != nil {
			return Publication{}, err
		}
		var everyone uint64
		known := false
		for _, r := range roles {
			if r.ID == m.GuildID {
				value, err := strconv.ParseUint(r.Permissions, 10, 64)
				if err != nil || known {
					return Publication{}, &SyncError{}
				}
				everyone, known = value, true
			}
		}
		if exists && !known {
			return Publication{}, &SyncError{}
		}
		for _, f := range m.Forums {
			selection.ForumIDs = append(selection.ForumIDs, f.ID)
			if !exists {
				continue
			}
			var parent discordChannel
			ok, err := c.read(ctx, "discord", "/channels/"+f.ID, &parent)
			if err != nil {
				return Publication{}, err
			}
			if !ok || parent.ID != f.ID || !publicForum(parent, m.GuildID, everyone) {
				continue
			}
			for _, id := range f.ThreadIDs {
				var thread discordChannel
				ok, err := c.read(ctx, "discord", "/channels/"+id, &thread)
				if err != nil {
					return Publication{}, err
				}
				if !ok || thread.ID != id {
					continue
				}
				if item, valid := publishedThread(thread, parent, f, m.GuildID, now); valid {
					p.Forums = append(p.Forums, item)
				}
			}
		}
	}
	for _, approved := range m.Issues {
		selection.IssueIDs = append(selection.IssueIDs, approved.IssueID)
		parts := strings.Split(approved.IssueID, "#")
		number, _ := strconv.Atoi(parts[1])
		var issue struct {
			Number      int             `json:"number"`
			State       string          `json:"state"`
			StateReason string          `json:"state_reason"`
			PullRequest json.RawMessage `json:"pull_request"`
			Labels      []struct {
				Name string `json:"name"`
			} `json:"labels"`
		}
		ok, err := c.read(ctx, "github", "/repos/"+parts[0]+"/issues/"+parts[1], &issue)
		if err != nil {
			return Publication{}, err
		}
		if !ok {
			continue
		}
		if issue.Number != number || len(issue.PullRequest) > 0 || len(issue.Labels) > 100 {
			return Publication{}, &SyncError{}
		}
		status := ""
		if issue.State == "open" {
			status = approved.OpenStatus
		} else if issue.State == "closed" && issue.StateReason == "completed" {
			status = approved.CompletedStatus
		} else {
			continue
		}
		mapped := ""
		for _, label := range issue.Labels {
			if s := approved.LabelStatuses[label.Name]; s != "" {
				if mapped != "" && mapped != s {
					return Publication{}, errors.New("ambiguous published issue status")
				}
				mapped = s
			}
		}
		if mapped != "" {
			status = mapped
		}
		if status == "" {
			continue
		}
		p.Roadmap = append(p.Roadmap, PublishedIssue{IssueID: approved.IssueID, ID: approved.ID, Title: approved.Title, Summary: approved.Summary, Status: status, Dependencies: approved.Dependencies, Date: approved.Date, DateConfirmed: approved.DateConfirmed, ThreadID: approved.ThreadID, LinkToIssue: approved.LinkToIssue})
	}
	// Permissions may change while selected threads and issues are being read.
	if len(p.Forums) > 0 {
		currentThreads := map[string]discordChannel{}
		for _, entry := range p.Forums {
			var thread discordChannel
			ok, err := c.read(ctx, "discord", "/channels/"+entry.ThreadID, &thread)
			if err != nil {
				return Publication{}, err
			}
			if ok && thread.ID == entry.ThreadID {
				currentThreads[entry.ThreadID] = thread
			}
		}
		var roles []struct {
			ID          string `json:"id"`
			Permissions string `json:"permissions"`
		}
		exists, err := c.read(ctx, "discord", "/guilds/"+m.GuildID+"/roles", &roles)
		if err != nil {
			return Publication{}, err
		}
		var everyone uint64
		known := false
		for _, role := range roles {
			if role.ID == m.GuildID {
				value, err := strconv.ParseUint(role.Permissions, 10, 64)
				if err != nil || known {
					return Publication{}, &SyncError{}
				}
				everyone, known = value, true
			}
		}
		allowed := map[string]bool{}
		parents := map[string]discordChannel{}
		if exists && !known {
			return Publication{}, &SyncError{}
		}
		if exists {
			for _, entry := range p.Forums {
				if _, checked := allowed[entry.ForumID]; checked {
					continue
				}
				var forum discordChannel
				ok, err := c.read(ctx, "discord", "/channels/"+entry.ForumID, &forum)
				if err != nil {
					return Publication{}, err
				}
				allowed[entry.ForumID] = ok && forum.ID == entry.ForumID && publicForum(forum, m.GuildID, everyone)
				parents[entry.ForumID] = forum
			}
		}
		retained := p.Forums[:0]
		for _, entry := range p.Forums {
			if !allowed[entry.ForumID] {
				continue
			}
			thread, exists := currentThreads[entry.ThreadID]
			if !exists {
				continue
			}
			for _, selected := range m.Forums {
				if selected.ID != entry.ForumID {
					continue
				}
				if item, valid := publishedThread(thread, parents[entry.ForumID], selected, m.GuildID, now); valid {
					retained = append(retained, item)
				}
				break
			}
		}
		p.Forums = retained
	}
	b, err := json.Marshal(p)
	if err != nil {
		return Publication{}, err
	}
	if _, err = Decode(b, selection, now); err != nil {
		return Publication{}, err
	}
	return p, nil
}

// SyncToFile replaces only the named artifact; a failed refresh withdraws it.
func (c SyncClient) SyncToFile(ctx context.Context, m Manifest, path string, now time.Time) (err error) {
	started := time.Now()
	if path == "" {
		return errors.New("publication path required")
	}
	if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
		return errors.New("publication must be a regular file")
	}
	coolingDown := false
	defer func() {
		if err != nil {
			var limited *SyncError
			if errors.As(err, &limited) && limited.RetryAfter > 0 && !coolingDown {
				deadline := now.Add(time.Since(started)).Add(limited.RetryAfter).Add(time.Second - 1)
				if writeErr := writeArtifact(path+".retry-after", []byte(strconv.FormatInt(deadline.Unix(), 10))); writeErr != nil {
					err = errors.New("rate limit received; unable to record retry deadline")
				}
			}
			if removeErr := Withdraw(path); removeErr != nil {
				err = errors.New("sync failed and old publication could not be withdrawn")
			}
		}
	}()
	if delay, readErr := retryDelay(path+".retry-after", now); readErr != nil {
		return readErr
	} else if delay > 0 {
		coolingDown = true
		return &SyncError{RetryAfter: delay}
	}
	p, err := c.Sync(ctx, m, now)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writeArtifact(path, body)
}

func writeArtifact(path string, body []byte) error {
	if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
		return errors.New("artifact must be a regular file")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".community-publication-*")
	if err != nil {
		return errors.New("cannot prepare publication")
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return errors.New("cannot write publication")
	}
	if err = os.Rename(name, path); err != nil {
		return errors.New("cannot replace publication")
	}
	return nil
}

func retryDelay(path string, now time.Time) (time.Duration, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, errors.New("cannot read retry deadline")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32 {
		return 0, errors.New("invalid retry deadline")
	}
	b, err := io.ReadAll(io.LimitReader(f, 33))
	if err != nil || len(b) > 32 {
		return 0, errors.New("invalid retry deadline")
	}
	deadline, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, errors.New("invalid retry deadline")
	}
	return time.Unix(deadline, 0).Sub(now), nil
}

// Withdraw never removes directories or follows symlinks.
func Withdraw(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("cannot withdraw publication artifact")
	}
	if os.Remove(path) != nil {
		return errors.New("cannot withdraw publication artifact")
	}
	return nil
}
