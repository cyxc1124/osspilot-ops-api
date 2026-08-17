package about

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultGitHubAPI = "https://api.github.com"
	cacheTTL         = 30 * time.Minute
	githubTimeout    = 5 * time.Second
)

type commitRef struct {
	SHA string
	URL string
}

type snapshot struct {
	Release    string
	ReleaseURL string
	Commits    map[string]commitRef
}

type cacheEntry struct {
	snap    snapshot
	at      time.Time
	present bool
}

type GitHub struct {
	base   string
	token  string
	client *http.Client

	mu       sync.Mutex
	cache    map[string]cacheEntry
	inflight bool
}

func NewGitHub(base, token string) *GitHub {
	if base == "" {
		base = defaultGitHubAPI
	}
	return &GitHub{
		base:   strings.TrimRight(base, "/"),
		token:  token,
		client: &http.Client{Timeout: githubTimeout},
		cache:  map[string]cacheEntry{},
	}
}

func (g *GitHub) cached(repo string) (snapshot, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.cache[repo]
	if !ok || !e.present {
		return snapshot{}, false
	}
	return e.snap, true
}

func (g *GitHub) maybeRefresh(owner string, comps []Component) {
	g.mu.Lock()
	stale := !g.inflight && (len(g.cache) == 0 || time.Since(oldestLocked(g.cache)) > cacheTTL)
	if stale {
		g.inflight = true
	}
	g.mu.Unlock()
	if !stale {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		g.refresh(ctx, owner, comps)
		g.mu.Lock()
		g.inflight = false
		g.mu.Unlock()
	}()
}

func oldestLocked(cache map[string]cacheEntry) time.Time {
	var t time.Time
	for _, e := range cache {
		if t.IsZero() || e.at.Before(t) {
			t = e.at
		}
	}
	if t.IsZero() {
		return time.Time{}
	}
	return t
}

func (g *GitHub) refresh(ctx context.Context, owner string, comps []Component) {
	needed := map[string]map[string]struct{}{}
	for _, c := range comps {
		if needed[c.Repo] == nil {
			needed[c.Repo] = map[string]struct{}{}
		}
		if c.GitBranch != nil && *c.GitBranch != "" {
			needed[c.Repo][*c.GitBranch] = struct{}{}
		}
	}
	for repo, branches := range needed {
		snap := g.fetch(ctx, owner, repo, branches)
		g.mu.Lock()
		g.cache[repo] = cacheEntry{snap: snap, at: time.Now(), present: true}
		g.mu.Unlock()
	}
}

func (g *GitHub) fetch(ctx context.Context, owner, repo string, branches map[string]struct{}) snapshot {
	s := snapshot{Commits: map[string]commitRef{}}
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := g.getJSON(ctx, "/repos/"+owner+"/"+repo+"/releases/latest", &rel); err == nil {
		s.Release = rel.TagName
		s.ReleaseURL = rel.HTMLURL
	}
	for branch := range branches {
		var c struct {
			SHA string `json:"sha"`
			URL string `json:"html_url"`
		}
		if err := g.getJSON(ctx, "/repos/"+owner+"/"+repo+"/commits/"+branch, &c); err == nil {
			s.Commits[branch] = commitRef{SHA: c.SHA, URL: c.URL}
		}
	}
	return s
}

func (g *GitHub) getJSON(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "osspilot-ops-api")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	res, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return errStatus(res.StatusCode)
	}
	return json.Unmarshal(body, dest)
}

type statusError int

func (e statusError) Error() string { return http.StatusText(int(e)) }

func errStatus(code int) error { return statusError(code) }
