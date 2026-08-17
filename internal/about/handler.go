package about

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
)

type Handler struct {
	protect func(auth.UserHandler) http.HandlerFunc
	github  *GitHub
	owner   string
	http    *http.Client
}

func NewHandler(protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{
		protect: protect,
		github:  NewGitHub(os.Getenv("OSSPILOT_GITHUB_API"), os.Getenv("OSSPILOT_GITHUB_TOKEN")),
		owner:   githubOwner(),
		http:    &http.Client{Timeout: 2 * time.Second},
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/about", h.protect(h.get))
}

func (h *Handler) get(w http.ResponseWriter, _ *http.Request, _ *auth.User) {
	httpx.JSON(w, http.StatusOK, h.response())
}

func (h *Handler) response() Response {
	comps := h.components()
	h.github.maybeRefresh(h.owner, comps)
	for i := range comps {
		h.apply(&comps[i])
	}
	return Response{
		AppName:    "OssPilot",
		BuildTime:  opt(env("BUILD_TIME")),
		Owner:      h.owner,
		Components: comps,
	}
}

func (h *Handler) components() []Component {
	specs := catalog()
	out := make([]Component, len(specs))
	var wg sync.WaitGroup
	for i, s := range specs {
		wg.Add(1)
		go func(i int, s spec) {
			defer wg.Done()
			out[i] = h.one(s)
		}(i, s)
	}
	wg.Wait()
	return out
}

func (h *Handler) one(s spec) Component {
	c := Component{
		ID:            s.ID,
		Name:          s.ID,
		Repo:          s.Repo,
		RepoURL:       "https://github.com/" + h.owner + "/" + s.Repo,
		CompareStatus: "checking",
	}
	var p parsed
	if s.URL == "" && s.ID == "ops-api" {
		p = selfBuild()
		if p.Display == "" {
			p = parsed{Display: "dev", Channel: "local"}
		}
		c.Reachable = true
	} else {
		var ok bool
		p, ok = h.probe(s)
		c.Reachable = ok
		if !ok {
			c.RunningVersion = ""
			c.Channel = "unknown"
			c.CompareStatus = "unknown"
			return c
		}
	}
	c.RunningVersion = p.Display
	c.GitTag = opt(p.Tag)
	c.GitBranch = opt(p.Branch)
	c.GitCommit = opt(p.Commit)
	c.Channel = p.Channel
	return c
}

type probeBody struct {
	Version   string `json:"version"`
	GitTag    string `json:"git_tag"`
	GitBranch string `json:"git_branch"`
	GitCommit string `json:"git_commit"`
}

func (h *Handler) probe(s spec) (parsed, bool) {
	if s.URL == "" || h.http == nil {
		return parsed{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.URL, "/")+s.Path, nil)
	if err != nil {
		return parsed{}, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "osspilot-ops-api")
	res, err := h.http.Do(req)
	if err != nil {
		return parsed{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return parsed{}, false
	}
	var body probeBody
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body); err != nil {
		return parsed{}, false
	}
	return fromProbe(body.GitTag, body.GitBranch, body.GitCommit, body.Version), true
}

func (h *Handler) apply(c *Component) {
	if !c.Reachable {
		c.CompareStatus = "unknown"
		return
	}
	snap, fetched := h.github.cached(c.Repo)
	if fetched {
		c.LatestRelease = opt(snap.Release)
		c.LatestReleaseURL = opt(snap.ReleaseURL)
		if c.GitBranch != nil {
			if tip, ok := snap.Commits[*c.GitBranch]; ok {
				c.LatestCommit = opt(short(tip.SHA))
				c.LatestCommitURL = opt(tip.URL)
			}
		}
	}
	tag, branch, commit := "", "", ""
	if c.GitTag != nil {
		tag = *c.GitTag
	}
	if c.GitBranch != nil {
		branch = *c.GitBranch
	}
	if c.GitCommit != nil {
		commit = *c.GitCommit
	}
	avail, url := decideUpdate(tag, branch, commit, snap)
	c.UpdateAvailable = avail
	c.UpdateURL = opt(url)
	c.CompareStatus = compareStatus(avail, fetched)
}
