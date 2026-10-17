package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/community"
)

func TestCommunityPublicationRequiresPlexAndOnlyExposesSelectedPublicSummary(t *testing.T) {
	s := fixture(t)
	s.Config.CommunityFeedFile = filepath.Join(t.TempDir(), "publication.json")
	s.Config.CommunityForumIDs = []string{"11111"}
	s.Config.RoadmapIssueIDs = []string{"private/project#1"}
	now := time.Now().UTC()
	p := community.Publication{Version: 1, PublishedAt: now, ExpiresAt: now.Add(time.Hour), Forums: []community.PublishedThread{{ForumID: "11111", ThreadID: "22222", Title: "Public proposal", Kind: "proposal", Status: "open", UpdatedAt: now}, {ForumID: "99999", Title: "SECRET PRIVATE DISCUSSION"}}, Roadmap: []community.PublishedIssue{{IssueID: "private/project#1", ID: "public-step", Title: "Public milestone", Status: "planned", Dependencies: []string{}}, {IssueID: "private/admin#2", Title: "SECRET PRIVATE ISSUE"}}}
	body, _ := json.Marshal(p)
	if err := os.WriteFile(s.Config.CommunityFeedFile, body, 0600); err != nil {
		t.Fatal(err)
	}
	cookie := seed(t, s, "11111", true)
	for _, path := range []string{"/api/v1/community", "/api/v1/roadmap"} {
		if w := call(s, "GET", path, "", "", ""); w.Code != 401 {
			t.Fatalf("anonymous publication %d", w.Code)
		}
		w := call(s, "GET", path, cookie, "", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "SECRET") || strings.Contains(w.Body.String(), "private/") || !strings.Contains(w.Body.String(), "Public") {
			t.Fatalf("unsafe publication %d %s", w.Code, w.Body)
		}
		if w = call(s, "GET", path+"?forumId=99999", cookie, "", ""); w.Code != 400 {
			t.Fatalf("selector bypass %d", w.Code)
		}
	}
	if err := os.Remove(s.Config.CommunityFeedFile); err != nil {
		t.Fatal(err)
	}
	if w := call(s, "GET", "/api/v1/community", cookie, "", ""); w.Code != 503 || strings.Contains(w.Body.String(), "Public proposal") || strings.Contains(w.Body.String(), s.Config.CommunityFeedFile) {
		t.Fatalf("missing publication leaked old content or path %d %s", w.Code, w.Body)
	}
}
