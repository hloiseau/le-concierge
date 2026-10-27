package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type AnimeTitle struct{ Romaji, English, Native string }
type AnimeMedia struct {
	ID                                int64
	Type, Format, Status, Description string
	Title                             AnimeTitle
	Synonyms, Genres                  []string
	Episodes, Volumes                 *int
	IsAdult                           bool
	StartDate                         struct{ Year, Month, Day *int }
}
type AnimeResult struct {
	Reference *AnimeMedia
	Items     []AnimeMedia
	Ambiguous bool
}

const animeFields = `id type format status title { romaji english native } synonyms genres episodes volumes isAdult startDate { year month day } description(asHtml:false)`

// Anime reads public catalogue queries only; no login, lists or user profile.
func (c *Client) Anime(ctx context.Context, query, kind, genre string, year int, recommend bool) (AnimeResult, error) {
	if kind != "anime" && kind != "manga" {
		return AnimeResult{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	typ := strings.ToUpper(kind)
	variables := map[string]any{"type": typ}
	graphql := ""
	if query != "" {
		variables["search"] = query
		graphql = `query($search:String!,$type:MediaType!){Page(page:1,perPage:5){media(search:$search,type:$type,isAdult:false){` + animeFields + `}}}`
	} else {
		variables["genre"] = nil
		if genre != "" {
			variables["genre"] = genre
		}
		graphql = `query($type:MediaType!,$genre:String){Page(page:1,perPage:3){media(type:$type,genre:$genre,sort:POPULARITY_DESC,isAdult:false){` + animeFields + `}}}`
	}
	var data struct {
		Data struct {
			Page  struct{ Media []AnimeMedia }
			Media struct {
				ID              int64
				Recommendations struct {
					Nodes []struct{ MediaRecommendation *AnimeMedia }
				}
			}
		}
		Errors []json.RawMessage
	}
	request := func(q string) error {
		body, _ := json.Marshal(map[string]any{"query": q, "variables": variables})
		data.Errors = nil
		if err := c.JSON(ctx, "POST", "https://graphql.anilist.co", map[string]string{"Content-Type": "application/json"}, string(body), &data); err != nil {
			return err
		}
		if len(data.Errors) > 0 {
			return ErrUnavailable
		}
		return nil
	}
	if err := request(graphql); err != nil {
		return AnimeResult{}, err
	}
	if len(data.Data.Page.Media) > 5 {
		return AnimeResult{}, ErrIncomplete
	}
	items := []AnimeMedia{}
	for _, m := range data.Data.Page.Media {
		if m.ID > 0 && m.Type == typ && !m.IsAdult {
			items = append(items, m)
		}
	}
	if query == "" {
		if len(items) > 3 {
			return AnimeResult{}, ErrIncomplete
		}
		return AnimeResult{Items: items}, nil
	}
	eligible, exact := []AnimeMedia{}, []AnimeMedia{}
	for _, m := range items {
		if year != 0 && (m.StartDate.Year == nil || *m.StartDate.Year != year) {
			continue
		}
		eligible = append(eligible, m)
		aliases := append([]string{m.Title.Romaji, m.Title.English, m.Title.Native}, m.Synonyms...)
		for _, alias := range aliases {
			if movieTitle(alias) == movieTitle(query) {
				exact = append(exact, m)
				break
			}
		}
	}
	if len(exact) > 0 {
		eligible = exact
	}
	if len(eligible) != 1 {
		return AnimeResult{Items: eligible, Ambiguous: true}, nil
	}
	ref := eligible[0]
	if !recommend {
		return AnimeResult{Items: []AnimeMedia{ref}}, nil
	}
	variables = map[string]any{"id": ref.ID}
	graphql = `query($id:Int!){Media(id:$id){id recommendations(page:1,perPage:5,sort:RATING_DESC){nodes{mediaRecommendation{` + animeFields + `}}}}}`
	if err := request(graphql); err != nil {
		return AnimeResult{}, err
	}
	if data.Data.Media.ID != ref.ID || len(data.Data.Media.Recommendations.Nodes) > 5 {
		return AnimeResult{}, ErrIncomplete
	}
	items = nil
	seen := map[int64]bool{ref.ID: true}
	for _, node := range data.Data.Media.Recommendations.Nodes {
		m := node.MediaRecommendation
		if m == nil || m.ID <= 0 || m.Type != typ || m.IsAdult || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		items = append(items, *m)
		if len(items) == 3 {
			break
		}
	}
	return AnimeResult{Reference: &ref, Items: items}, nil
}

func (m AnimeMedia) DisplayTitle() string {
	for _, title := range []string{m.Title.English, m.Title.Romaji, m.Title.Native} {
		if strings.TrimSpace(title) != "" {
			return cleanActivity(title, 140)
		}
	}
	return fmt.Sprintf("Œuvre AniList %d", m.ID)
}
