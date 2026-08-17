package about

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want parsed
	}{
		{"", parsed{Display: "dev", Channel: "local"}},
		{"dev", parsed{Display: "dev", Channel: "local"}},
		{"v1.0.1", parsed{Display: "v1.0.1", Tag: "v1.0.1", Channel: "release"}},
		{"1.2.0", parsed{Display: "v1.2.0", Tag: "v1.2.0", Channel: "release"}},
		{"develop", parsed{Display: "develop", Branch: "develop", Channel: "develop"}},
		{"develop@abcdef12", parsed{Display: "develop@abcdef12", Branch: "develop", Commit: "abcdef12", Channel: "develop"}},
		{"sha-abcdef12ffff", parsed{Display: "abcdef12", Commit: "abcdef12", Channel: "sha"}},
		{"abcdef12", parsed{Display: "abcdef12", Commit: "abcdef12", Channel: "sha"}},
	}
	for _, tc := range cases {
		got := parseVersion(tc.in)
		if got != tc.want {
			t.Fatalf("%q: got %#v want %#v", tc.in, got, tc.want)
		}
	}
}

func TestDecideUpdate(t *testing.T) {
	rel := snapshot{Release: "v1.0.1", ReleaseURL: "https://example/rel"}
	avail, url := decideUpdate("v1.0.0", "", "", rel)
	if avail == nil || !*avail || url == "" {
		t.Fatalf("older tag: avail=%v url=%q", avail, url)
	}
	avail, url = decideUpdate("v1.0.1", "", "", rel)
	if avail == nil || *avail || url != "" {
		t.Fatalf("same tag: avail=%v url=%q", avail, url)
	}

	tip := snapshot{Commits: map[string]commitRef{"develop": {SHA: "bbbbbbbbffff", URL: "https://example/c"}}}
	avail, url = decideUpdate("", "develop", "aaaaaaaa", tip)
	if avail == nil || !*avail || url == "" {
		t.Fatalf("behind commit: avail=%v url=%q", avail, url)
	}
	avail, url = decideUpdate("", "develop", "bbbbbbbb", tip)
	if avail == nil || *avail || url != "" {
		t.Fatalf("same commit: avail=%v url=%q", avail, url)
	}
	avail, _ = decideUpdate("", "develop", "", tip)
	if avail != nil {
		t.Fatal("branch without commit cannot compare")
	}
}

func TestSelfUsesBakedTag(t *testing.T) {
	t.Setenv("GIT_TAG", "v1.0.0")
	t.Setenv("TENANT_API_URL", "")
	t.Setenv("OSSPILOT_TENANT_API_URL", "")
	h := &Handler{github: NewGitHub("", ""), owner: "cyxc1124", http: http.DefaultClient}
	comps := h.components()
	byID := map[string]Component{}
	for _, c := range comps {
		byID[c.ID] = c
	}
	if !byID["ops-api"].Reachable || byID["ops-api"].RunningVersion != "v1.0.0" {
		t.Fatalf("ops-api %#v", byID["ops-api"])
	}
	if byID["tenant-api"].Reachable {
		t.Fatalf("tenant-api should be unreachable without URL: %#v", byID["tenant-api"])
	}
}

func TestProbeReadsSibling(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":     "ok",
			"version":    "v0.9.0",
			"git_tag":    "v0.9.0",
			"git_commit": "aaaaaaaa",
		})
	}))
	t.Cleanup(peer.Close)
	t.Setenv("GIT_TAG", "v1.0.0")
	t.Setenv("TENANT_API_URL", peer.URL)
	h := &Handler{github: NewGitHub("", ""), owner: "cyxc1124", http: peer.Client()}
	comps := h.components()
	var tenant Component
	for _, c := range comps {
		if c.ID == "tenant-api" {
			tenant = c
		}
	}
	if !tenant.Reachable || tenant.RunningVersion != "v0.9.0" {
		t.Fatalf("tenant-api %#v", tenant)
	}
}

func TestRefreshMarksUpdate(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"tag_name": "v1.0.1",
				"html_url": "https://github.com/cyxc1124/osspilot-ops-api/releases/tag/v1.0.1",
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(gh.Close)
	t.Setenv("GIT_TAG", "v1.0.0")
	h := &Handler{github: NewGitHub(gh.URL, ""), owner: "cyxc1124", http: http.DefaultClient}
	comps := h.components()
	h.github.refresh(context.Background(), h.owner, comps)
	for i := range comps {
		h.apply(&comps[i])
	}
	var ops Component
	for _, c := range comps {
		if c.ID == "ops-api" {
			ops = c
		}
	}
	if ops.CompareStatus != "update" || ops.UpdateAvailable == nil || !*ops.UpdateAvailable {
		t.Fatalf("ops-api %#v", ops)
	}
}

func TestAboutHTTP(t *testing.T) {
	t.Setenv("GIT_TAG", "v1.0.0")
	h := &Handler{
		protect: func(next auth.UserHandler) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				next(w, r, &auth.User{ID: 1, Username: "admin", Status: "active"})
			}
		},
		github: NewGitHub("", ""),
		owner:  "cyxc1124",
		http:   http.DefaultClient,
	}
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/about", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body Response
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.AppName != "OssPilot" || len(body.Components) != 6 {
		t.Fatalf("body %#v", body)
	}
	if body.Components[3].ID != "ops-api" || body.Components[3].RunningVersion != "v1.0.0" {
		t.Fatalf("ops-api %#v", body.Components[3])
	}
}
