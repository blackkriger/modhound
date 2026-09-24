package modrinth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/blackkriger/modhound/internal/httpx"
)

const baseURL = "https://api.modrinth.com/v2"

type File struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Hashes   struct {
		SHA1   string `json:"sha1"`
		SHA512 string `json:"sha512"`
	} `json:"hashes"`
}

type Version struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	VersionNumber string    `json:"version_number"`
	VersionType   string    `json:"version_type"`
	DatePublished time.Time `json:"date_published"`
	Changelog     string    `json:"changelog"`
	Files         []File    `json:"files"`
}

func (v *Version) PrimaryFile() *File {
	for i := range v.Files {
		if v.Files[i].Primary {
			return &v.Files[i]
		}
	}
	if len(v.Files) > 0 {
		return &v.Files[0]
	}
	return nil
}

type Project struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
}

func do(ctx context.Context, method, path string, body, out any) error {
	return httpx.Do(ctx, method, baseURL+path, nil, body, out)
}

func VersionsByHash(ctx context.Context, sha1s []string) (map[string]Version, error) {
	out := map[string]Version{}
	if len(sha1s) == 0 {
		return out, nil
	}
	err := do(ctx, http.MethodPost, "/version_files", map[string]any{"hashes": sha1s, "algorithm": "sha1"}, &out)
	return out, err
}

func Versions(ctx context.Context, projectID, loader, gameVersion string) ([]Version, error) {
	q := url.Values{}
	loaders, _ := json.Marshal([]string{loader})
	games, _ := json.Marshal([]string{gameVersion})
	q.Set("loaders", string(loaders))
	q.Set("game_versions", string(games))
	var out []Version
	err := do(ctx, http.MethodGet, "/project/"+url.PathEscape(projectID)+"/version?"+q.Encode(), nil, &out)
	return out, err
}

func Projects(ctx context.Context, ids []string) (map[string]Project, error) {
	out := map[string]Project{}
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		b, _ := json.Marshal(ids[start:end])
		var list []Project
		if err := do(ctx, http.MethodGet, "/projects?ids="+url.QueryEscape(string(b)), nil, &list); err != nil {
			return nil, err
		}
		for _, p := range list {
			out[p.ID] = p
		}
	}
	return out, nil
}

func ProjectURL(slug string) string {
	return "https://modrinth.com/mod/" + slug
}
