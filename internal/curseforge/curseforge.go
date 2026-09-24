package curseforge

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/blackkriger/modhound/internal/httpx"
)

const (
	baseURL       = "https://api.curseforge.com/v1"
	minecraftGame = 432

	ReleaseTypeRelease = 1

	RelationRequired = 3

	hashSHA1 = 1
)

type Client struct {
	Key string
}

type Hash struct {
	Value string `json:"value"`
	Algo  int    `json:"algo"`
}

type Dependency struct {
	ModID        int `json:"modId"`
	RelationType int `json:"relationType"`
}

type File struct {
	ID           int          `json:"id"`
	DisplayName  string       `json:"displayName"`
	FileName     string       `json:"fileName"`
	ReleaseType  int          `json:"releaseType"`
	Hashes       []Hash       `json:"hashes"`
	FileDate     time.Time    `json:"fileDate"`
	DownloadURL  string       `json:"downloadUrl"`
	GameVersions []string     `json:"gameVersions"`
	Dependencies []Dependency `json:"dependencies"`
	IsAvailable  bool         `json:"isAvailable"`
	Fingerprint  uint32       `json:"fileFingerprint"`
}

func (f *File) SHA1() string {
	for _, h := range f.Hashes {
		if h.Algo == hashSHA1 {
			return h.Value
		}
	}
	return ""
}

type Author struct {
	Name string `json:"name"`
}

type Mod struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Summary string `json:"summary"`
	Links   struct {
		WebsiteURL string `json:"websiteUrl"`
	} `json:"links"`
	Authors []Author `json:"authors"`
	Logo    *struct {
		ThumbnailURL string `json:"thumbnailUrl"`
		URL          string `json:"url"`
	} `json:"logo"`
}

type Match struct {
	ModID int  `json:"id"`
	File  File `json:"file"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return httpx.Do(ctx, method, baseURL+path, map[string]string{"x-api-key": c.Key}, body, out)
}

func (c *Client) MatchFingerprints(ctx context.Context, fps []uint32) (map[uint32]Match, error) {
	out := make(map[uint32]Match)
	for start := 0; start < len(fps); start += 500 {
		end := min(start+500, len(fps))
		var resp struct {
			Data struct {
				ExactMatches []Match `json:"exactMatches"`
			} `json:"data"`
		}
		body := map[string]any{"fingerprints": fps[start:end]}
		if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/fingerprints/%d", minecraftGame), body, &resp); err != nil {
			return nil, err
		}
		for _, m := range resp.Data.ExactMatches {
			out[m.File.Fingerprint] = m
		}
	}
	return out, nil
}

func (c *Client) Mods(ctx context.Context, ids []int) (map[int]Mod, error) {
	out := make(map[int]Mod)
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		var resp struct {
			Data []Mod `json:"data"`
		}
		body := map[string]any{"modIds": ids[start:end], "filterPcOnly": true}
		if err := c.do(ctx, http.MethodPost, "/mods", body, &resp); err != nil {
			return nil, err
		}
		for _, m := range resp.Data {
			out[m.ID] = m
		}
	}
	return out, nil
}

func (c *Client) Files(ctx context.Context, modID int, gameVersion string) ([]File, error) {
	var all []File
	for index := 0; index < 1000; {
		q := url.Values{}
		q.Set("gameVersion", gameVersion)
		q.Set("pageSize", "50")
		q.Set("index", fmt.Sprint(index))
		var resp struct {
			Data       []File `json:"data"`
			Pagination struct {
				ResultCount int `json:"resultCount"`
				TotalCount  int `json:"totalCount"`
			} `json:"pagination"`
		}
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/mods/%d/files?%s", modID, q.Encode()), nil, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Data...)
		index += resp.Pagination.ResultCount
		if resp.Pagination.ResultCount == 0 || index >= resp.Pagination.TotalCount {
			break
		}
	}
	return all, nil
}

func (c *Client) Changelog(ctx context.Context, modID, fileID int) (string, error) {
	var resp struct {
		Data string `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/mods/%d/files/%d/changelog", modID, fileID), nil, &resp); err != nil {
		return "", err
	}
	return resp.Data, nil
}

func FilePageURL(slug string, fileID int) string {
	return fmt.Sprintf("https://www.curseforge.com/minecraft/mc-mods/%s/files/%d", slug, fileID)
}

func (c *Client) File(ctx context.Context, modID, fileID int) (*File, error) {
	var resp struct {
		Data File `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/mods/%d/files/%d", modID, fileID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
