package about

import (
	"net/http"
	"os"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/httpx"
)

type Handler struct {
	protect func(auth.UserHandler) http.HandlerFunc
	github  *GitHub
	owner   string
}

func NewHandler(protect func(auth.UserHandler) http.HandlerFunc) *Handler {
	return &Handler{
		protect: protect,
		github:  NewGitHub(os.Getenv("OSSPILOT_GITHUB_API"), os.Getenv("OSSPILOT_GITHUB_TOKEN")),
		owner:   githubOwner(),
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
	owner := h.owner
	out := make([]Component, 0, 6)
	for _, s := range catalog() {
		p := running(s)
		out = append(out, Component{
			ID:             s.ID,
			Name:           s.ID,
			Repo:           s.Repo,
			RepoURL:        "https://github.com/" + owner + "/" + s.Repo,
			RunningVersion: p.Display,
			GitTag:         opt(p.Tag),
			GitBranch:      opt(p.Branch),
			GitCommit:      opt(p.Commit),
			Channel:        p.Channel,
			CompareStatus:  "checking",
		})
	}
	return out
}

func (h *Handler) apply(c *Component) {
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
