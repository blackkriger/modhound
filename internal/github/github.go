package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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

type Result struct {
	Releases []Release
	Stale    error
}

func Releases(ctx context.Context, repo, cacheDir string) (Result, error) {
	url := "https://api.github.com/repos/" + repo + "/releases?per_page=100"
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if cacheDir == "" {
		var out []Release
		err := httpx.Do(ctx, http.MethodGet, url, headers, nil, &out)
		return Result{Releases: out}, explain(err)
	}
	cachePath := filepath.Join(cacheDir, "github", strings.ReplaceAll(repo, "/", "_")+".json")
	res, err := httpx.CachedGet(ctx, url, cachePath, time.Hour, headers, json.Valid)
	if err != nil {
		return Result{}, explain(err)
	}
	var out []Release
	if err := json.Unmarshal(res.Data, &out); err != nil {
		os.Remove(cachePath)
		os.Remove(cachePath + ".etag")
		return Result{}, err
	}
	return Result{Releases: out, Stale: explain(res.Stale)}, nil
}

func explain(err error) error {
	var se *httpx.StatusError
	if !errors.As(err, &se) {
		return err
	}
	if (se.Status == http.StatusForbidden || se.Status == http.StatusTooManyRequests) && se.Header.Get("X-RateLimit-Remaining") == "0" {
		if reset, perr := strconv.ParseInt(se.Header.Get("X-RateLimit-Reset"), 10, 64); perr == nil {
			return &apiError{fmt.Sprintf("GitHub HTTP %d: the limit of %s requests per hour is used up, it resets at %s", se.Status, se.Header.Get("X-RateLimit-Limit"), time.Unix(reset, 0).Format("15:04")), err}
		}
	}
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(se.Body), &body) == nil && body.Message != "" {
		return &apiError{fmt.Sprintf("GitHub HTTP %d: %s", se.Status, body.Message), err}
	}
	return &apiError{fmt.Sprintf("GitHub HTTP %d", se.Status), err}
}

type apiError struct {
	msg string
	err error
}

func (e *apiError) Error() string { return e.msg }

func (e *apiError) Unwrap() error { return e.err }

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
