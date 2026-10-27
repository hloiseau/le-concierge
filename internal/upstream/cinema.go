package upstream

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type CinemaCredit struct {
	Name, Character string
}

type CinemaDetails struct {
	Reference  RequestCandidate
	Choices    []RequestCandidate
	Overview   string
	Creators   []string
	Cast       []CinemaCredit
	Minutes    int
	TrailerURL string
	SourceURL  string
	CheckedAt  time.Time
}

var trailerKey = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Cinema retains catalogue metadata only. No requests, viewers or watch providers.
func (c *Client) Cinema(ctx context.Context, base, key string, user int64, query, kind string, year int) (CinemaDetails, error) {
	if year != 0 && (year < 1801 || year > 2999) {
		return CinemaDetails{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	matches, err := c.searchRequestCandidates(ctx, base, key, user, query, kind, 20)
	if err != nil {
		return CinemaDetails{}, err
	}
	var eligible, exact []RequestCandidate
	for _, m := range matches {
		if year != 0 && m.Year != strconv.Itoa(year) {
			continue
		}
		eligible = append(eligible, m)
		if movieTitle(m.Title) == movieTitle(query) {
			exact = append(exact, m)
		}
	}
	if len(exact) > 0 {
		eligible = exact
	}
	if len(eligible) != 1 {
		return CinemaDetails{Choices: eligible}, ErrMediaReference
	}
	m := eligible[0]
	m.Title = cleanActivity(m.Title, 140)
	headers, err := c.seerrMember(ctx, base, key, user)
	if err != nil {
		return CinemaDetails{}, err
	}
	var detail struct {
		ID             int64
		Title, Name    string
		Overview       string
		Runtime        int
		EpisodeRunTime []int                   `json:"episodeRunTime"`
		CreatedBy      []struct{ Name string } `json:"createdBy"`
		Credits        struct {
			Cast []struct {
				Name, Character string
			} `json:"cast"`
			Crew []struct{ Name, Job string } `json:"crew"`
		} `json:"credits"`
		RelatedVideos []struct{ Key, Site, Type string } `json:"relatedVideos"`
	}
	if err := c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/%s/%d?language=fr-FR", base, m.Kind, m.ID), headers, "", &detail); err != nil {
		return CinemaDetails{}, err
	}
	if detail.ID != m.ID || (m.Kind == "movie" && strings.TrimSpace(detail.Title) == "") || (m.Kind == "tv" && strings.TrimSpace(detail.Name) == "") {
		return CinemaDetails{}, ErrIncomplete
	}
	out := CinemaDetails{Reference: m, Overview: cleanActivity(detail.Overview, 400), Minutes: detail.Runtime, SourceURL: fmt.Sprintf("https://www.themoviedb.org/%s/%d", m.Kind, m.ID), CheckedAt: time.Now().UTC()}
	if m.Kind == "tv" && len(detail.EpisodeRunTime) > 0 {
		out.Minutes = detail.EpisodeRunTime[0]
	}
	if out.Minutes < 1 || out.Minutes > 600 {
		out.Minutes = 0
	}
	addCreator := func(name string) {
		name = cleanActivity(name, 80)
		for _, n := range out.Creators {
			if n == name {
				return
			}
		}
		if name != "" && len(out.Creators) < 3 {
			out.Creators = append(out.Creators, name)
		}
	}
	if m.Kind == "movie" {
		for _, person := range detail.Credits.Crew {
			if person.Job == "Director" {
				addCreator(person.Name)
			}
		}
	} else {
		for _, person := range detail.CreatedBy {
			addCreator(person.Name)
		}
	}
	for _, person := range detail.Credits.Cast {
		if len(out.Cast) == 5 {
			break
		}
		if name := cleanActivity(person.Name, 60); name != "" {
			out.Cast = append(out.Cast, CinemaCredit{Name: name, Character: cleanActivity(person.Character, 40)})
		}
	}
	for _, video := range detail.RelatedVideos {
		if video.Site == "YouTube" && video.Type == "Trailer" && trailerKey.MatchString(video.Key) {
			out.TrailerURL = "https://www.youtube.com/watch?v=" + video.Key
			break
		}
	}
	return out, nil
}
