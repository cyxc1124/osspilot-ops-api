package about

import (
	"os"
	"regexp"
	"strings"
)

const defaultOwner = "cyxc1124"

type spec struct {
	ID   string
	Repo string
	URL  string
	Path string
}

type parsed struct {
	Display string
	Tag     string
	Branch  string
	Commit  string
	Channel string
}

type Component struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Repo             string  `json:"repo"`
	RepoURL          string  `json:"repo_url"`
	RunningVersion   string  `json:"running_version"`
	GitTag           *string `json:"git_tag"`
	GitBranch        *string `json:"git_branch"`
	GitCommit        *string `json:"git_commit"`
	Channel          string  `json:"channel"`
	LatestRelease    *string `json:"latest_release"`
	LatestReleaseURL *string `json:"latest_release_url"`
	LatestCommit     *string `json:"latest_commit"`
	LatestCommitURL  *string `json:"latest_commit_url"`
	UpdateAvailable  *bool   `json:"update_available"`
	UpdateURL        *string `json:"update_url"`
	CompareStatus    string  `json:"compare_status"`
	Reachable        bool    `json:"reachable"`
}

type Response struct {
	AppName    string      `json:"app_name"`
	BuildTime  *string     `json:"build_time"`
	Owner      string      `json:"github_owner"`
	Components []Component `json:"components"`
}

var (
	semverRe = regexp.MustCompile(`^v?\d+\.\d+`)
	shaRe    = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
)

func catalog() []spec {
	tenantAPI := env("OSSPILOT_TENANT_API_URL")
	if tenantAPI == "" {
		tenantAPI = env("TENANT_API_URL")
	}
	return []spec{
		{ID: "tenant-api", Repo: "osspilot-tenant-api", URL: tenantAPI, Path: "/healthz"},
		{ID: "tenant-worker", Repo: "osspilot-tenant-worker", URL: env("OSSPILOT_TENANT_WORKER_URL"), Path: "/healthz"},
		{ID: "tenant-web", Repo: "osspilot-tenant-web", URL: env("OSSPILOT_TENANT_WEB_URL"), Path: "/version.json"},
		{ID: "ops-api", Repo: "osspilot-ops-api"},
		{ID: "ops-worker", Repo: "osspilot-ops-worker", URL: env("OSSPILOT_OPS_WORKER_URL"), Path: "/healthz"},
		{ID: "ops-web", Repo: "osspilot-ops-web", URL: env("OSSPILOT_OPS_WEB_URL"), Path: "/version.json"},
	}
}

func githubOwner() string {
	if v := env("OSSPILOT_GITHUB_OWNER"); v != "" {
		return v
	}
	return defaultOwner
}

func env(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func selfBuild() parsed {
	tag := env("GIT_TAG")
	branch := env("GIT_BRANCH")
	commit := short(env("GIT_COMMIT"))
	if tag != "" {
		return parseVersion(tag)
	}
	if branch != "" && commit != "" {
		return parsed{Display: branch + "@" + commit, Branch: branch, Commit: commit, Channel: channelForBranch(branch)}
	}
	if branch != "" {
		return parseVersion(branch)
	}
	if commit != "" {
		return parsed{Display: commit, Commit: commit, Channel: "sha"}
	}
	return parsed{}
}

func fromProbe(tag, branch, commit, version string) parsed {
	commit = short(commit)
	if tag != "" {
		p := parseVersion(tag)
		p.Commit = commit
		if version != "" {
			p.Display = version
		}
		return p
	}
	if branch != "" && commit != "" {
		display := version
		if display == "" {
			display = branch + "@" + commit
		}
		return parsed{Display: display, Branch: branch, Commit: commit, Channel: channelForBranch(branch)}
	}
	if version != "" {
		return parseVersion(version)
	}
	return parsed{Display: "dev", Channel: "local"}
}

func parseVersion(raw string) parsed {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "dev" {
		return parsed{Display: "dev", Channel: "local"}
	}
	if i := strings.IndexByte(raw, '@'); i > 0 {
		branch, commit := raw[:i], short(raw[i+1:])
		return parsed{Display: branch + "@" + commit, Branch: branch, Commit: commit, Channel: channelForBranch(branch)}
	}
	if strings.HasPrefix(raw, "sha-") {
		c := short(strings.TrimPrefix(raw, "sha-"))
		return parsed{Display: c, Commit: c, Channel: "sha"}
	}
	if semverRe.MatchString(raw) {
		tag := raw
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		return parsed{Display: tag, Tag: tag, Channel: "release"}
	}
	if raw == "develop" || raw == "main" {
		return parsed{Display: raw, Branch: raw, Channel: raw}
	}
	if shaRe.MatchString(raw) {
		c := short(raw)
		return parsed{Display: c, Commit: c, Channel: "sha"}
	}
	return parsed{Display: raw, Channel: "unknown"}
}

func channelForBranch(branch string) string {
	switch branch {
	case "develop", "main":
		return branch
	default:
		return "unknown"
	}
}

func short(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}

func normalizeTag(tag string) string {
	return strings.TrimPrefix(strings.TrimSpace(tag), "v")
}

func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func decideUpdate(tag, branch, commit string, s snapshot) (*bool, string) {
	if tag != "" && s.Release != "" {
		ok := normalizeTag(tag) != normalizeTag(s.Release)
		if ok {
			return &ok, s.ReleaseURL
		}
		return &ok, ""
	}
	if branch != "" && commit != "" {
		tip, ok := s.Commits[branch]
		if !ok || tip.SHA == "" {
			return nil, ""
		}
		behind := !strings.EqualFold(short(commit), short(tip.SHA))
		if behind {
			return &behind, tip.URL
		}
		return &behind, ""
	}
	return nil, ""
}

func compareStatus(avail *bool, fetched bool) string {
	if avail == nil {
		if fetched {
			return "unknown"
		}
		return "checking"
	}
	if *avail {
		return "update"
	}
	return "current"
}
