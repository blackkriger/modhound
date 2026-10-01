package github

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/blackkriger/modhound/internal/httpx"
)

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type Release struct {
	Tag        string    `json:"tag_name"`
	HTMLURL    string    `json:"html_url"`
	Published  time.Time `json:"published_at"`
	Prerelease bool      `json:"prerelease"`
	Draft      bool      `json:"draft"`
	Body       string    `json:"body"`
	Assets     []Asset   `json:"assets"`
}

func Releases(ctx context.Context, repo string) ([]Release, error) {
	var out []Release
	err := httpx.Do(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases?per_page=100", map[string]string{"Accept": "application/vnd.github+json"}, nil, &out)
	return out, err
}

func (r *Release) Jars(stem func(string) string, want string) []Asset {
	var out []Asset
	for _, a := range r.Assets {
		lower := strings.ToLower(a.Name)
		if !strings.HasSuffix(lower, ".jar") || strings.Contains(lower, "-sources") || strings.Contains(lower, "-dev") || strings.Contains(lower, "-api") {
			continue
		}
		if want == "" || stem(a.Name) == want {
			out = append(out, a)
		}
	}
	return out
}

func RepoURL(repo string) string {
	return "https://github.com/" + repo
}
