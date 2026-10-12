package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListSkillsPublicLeaderboardContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/all-time/1" || r.URL.RawQuery != "" {
			t.Errorf("unexpected leaderboard request: %s", r.URL)
		}
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") != "racomi" {
			t.Errorf("missing API headers: %v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"skills":[{"source":"owner/repo","skillId":"alpha","name":"Alpha","installs":1},{"source":"other/repo","skillId":"beta","name":"Beta","installs":4},{"source":"skills.example.com","skillId":"hosted","name":"Hosted","installs":0}],"total":500,"hasMore":true,"page":1}`)
	}))
	defer server.Close()
	client := New(t.TempDir())
	client.searchBase = server.URL
	page, err := client.ListSkills(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 500 || !page.HasMore || page.Page != 1 || len(page.Skills) != 3 {
		t.Fatalf("leaderboard pagination was lost: %#v", page)
	}
	if page.Skills[0].Slug != "owner/repo/alpha" || page.Skills[1].Slug != "other/repo/beta" || page.Skills[0].URL != "https://skills.sh/owner/repo/alpha" {
		t.Fatalf("leaderboard IDs or server order were lost: %#v", page.Skills)
	}
	if !page.Skills[0].Supported || page.Skills[2].Supported || page.Skills[2].UnsupportedReason == "" {
		t.Fatalf("hosted source support status was lost: %#v", page.Skills)
	}
}

func TestEmptySearchLoadsFirstLeaderboardPage(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/skills/all-time/0" || r.URL.RawQuery != "" {
			t.Errorf("empty search used a fabricated search query: %s", r.URL)
		}
		fmt.Fprint(w, `{"skills":[{"source":"owner/repo","skillId":"alpha","name":"alpha","installs":1},{"source":"owner/repo","skillId":"beta","name":"beta","installs":4}],"total":2,"hasMore":false,"page":0}`)
	}))
	defer server.Close()
	client := New(t.TempDir())
	client.searchBase = server.URL
	for _, query := range []string{"", "  \t\n"} {
		results, err := client.Search(context.Background(), query)
		if err != nil || len(results) != 2 || results[0].Name != "alpha" || results[1].Name != "beta" {
			t.Fatalf("empty search did not preserve leaderboard order: %#v, %v", results, err)
		}
	}
	if requests != 2 {
		t.Fatalf("unexpected number of leaderboard page requests: %d", requests)
	}
}

func TestListSkillsRejectsNegativePageBeforeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("invalid page reached the remote API")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := New(t.TempDir())
	client.searchBase = server.URL
	if _, err := client.ListSkills(context.Background(), -1); err == nil {
		t.Fatal("accepted a negative leaderboard page")
	}
}

func TestListSkillsErrorsAreVisible(t *testing.T) {
	tooMany, err := json.Marshal(struct {
		Skills  []searchSkill `json:"skills"`
		Total   int           `json:"total"`
		HasMore bool          `json:"hasMore"`
		Page    int           `json:"page"`
	}{Skills: make([]searchSkill, skillListPageSize+1), Total: skillListPageSize + 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, response := range []struct {
		name   string
		status int
		body   string
	}{
		{"rate limit", 429, "rate limited"},
		{"malformed JSON", 200, `{`},
		{"missing skills", 200, `{"total":0,"hasMore":false,"page":0}`},
		{"null skills", 200, `{"skills":null,"total":0,"hasMore":false,"page":0}`},
		{"missing total", 200, `{"skills":[],"hasMore":false,"page":0}`},
		{"missing hasMore", 200, `{"skills":[],"total":0,"page":0}`},
		{"missing page", 200, `{"skills":[],"total":0,"hasMore":false}`},
		{"wrong page", 200, `{"skills":[],"total":0,"hasMore":false,"page":1}`},
		{"negative total", 200, `{"skills":[],"total":-1,"hasMore":false,"page":0}`},
		{"empty incomplete page", 200, `{"skills":[],"total":1,"hasMore":true,"page":0}`},
		{"too many results", 200, string(tooMany)},
		{"unsafe source", 200, `{"skills":[{"source":"--exec","skillId":"alpha","name":"alpha","installs":0}],"total":1,"hasMore":false,"page":0}`},
		{"unsafe skill ID", 200, `{"skills":[{"source":"owner/repo","skillId":"../alpha","name":"alpha","installs":0}],"total":1,"hasMore":false,"page":0}`},
		{"missing skill ID", 200, `{"skills":[{"source":"owner/repo","name":"alpha","installs":0}],"total":1,"hasMore":false,"page":0}`},
		{"invalid name", 200, `{"skills":[{"source":"owner/repo","skillId":"alpha","name":"alpha\nunsafe","installs":0}],"total":1,"hasMore":false,"page":0}`},
		{"negative installs", 200, `{"skills":[{"source":"owner/repo","skillId":"alpha","name":"alpha","installs":-1}],"total":1,"hasMore":false,"page":0}`},
		{"inconsistent total", 200, `{"skills":[{"source":"owner/repo","skillId":"alpha","name":"alpha","installs":0}],"total":0,"hasMore":false,"page":0}`},
		{"too large", 200, strings.Repeat(" ", maxSearchBytes+1)},
	} {
		t.Run(response.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(response.status)
				fmt.Fprint(w, response.body)
			}))
			defer server.Close()
			client := New(t.TempDir())
			client.searchBase = server.URL
			if _, err := client.ListSkills(context.Background(), 0); err == nil {
				t.Fatal("leaderboard failure was silently returned as empty results")
			}
		})
	}
}

func TestListSkillsAcceptsEmptyFinalPageAndHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"skills":[],"total":0,"hasMore":false,"page":0}`)
	}))
	defer server.Close()
	client := New(t.TempDir())
	client.searchBase = server.URL
	page, err := client.ListSkills(context.Background(), 0)
	if err != nil || page.Skills == nil || len(page.Skills) != 0 || page.HasMore {
		t.Fatalf("valid empty leaderboard was rejected: %#v, %v", page, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ListSkills(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("leaderboard ignored request cancellation: %v", err)
	}
}
