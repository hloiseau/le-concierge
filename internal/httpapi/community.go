package httpapi

import (
	"net/http"
	"time"

	"github.com/hloiseau/le-concierge/internal/community"
)

func (s *Server) getCommunity(w http.ResponseWriter, r *http.Request) {
	s.publishedCommunity(w, r, false)
}
func (s *Server) getRoadmap(w http.ResponseWriter, r *http.Request) { s.publishedCommunity(w, r, true) }

func (s *Server) publishedCommunity(w http.ResponseWriter, r *http.Request, roadmap bool) {
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Seule la sélection publiée est disponible.")
		return
	}
	if s.Config.CommunityFeedFile == "" || (roadmap && len(s.Config.RoadmapIssueIDs) == 0) || (!roadmap && len(s.Config.CommunityForumIDs) == 0) {
		problem(w, 503, "not_configured", "Cette publication n’est pas encore configurée.")
		return
	}
	if s.limited(w, r, "community", 30) {
		return
	}
	data, err := community.Load(s.Config.CommunityFeedFile, community.Selection{GuildID: s.Config.DiscordGuildID, ForumIDs: s.Config.CommunityForumIDs, IssueIDs: s.Config.RoadmapIssueIDs}, time.Now())
	if err != nil {
		problem(w, 503, "publication_unavailable", "La publication est temporairement indisponible ou doit être actualisée.")
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		return
	}
	var items any = data.Threads
	if roadmap {
		items = data.Milestones
	}
	jsonOut(w, 200, map[string]any{"items": items, "publishedAt": data.PublishedAt, "expiresAt": data.ExpiresAt})
}
