package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blackkriger/modhound/internal/logx"
)

type Cached struct {
	Data  []byte
	Stale error
}

func CachedGet(ctx context.Context, url, cachePath string, maxAge time.Duration, headers map[string]string, valid func([]byte) bool) (Cached, error) {
	etagPath := cachePath + ".etag"
	cached, cacheErr := os.ReadFile(cachePath)
	if cacheErr == nil && maxAge > 0 {
		if info, err := os.Stat(cachePath); err == nil && time.Since(info.ModTime()) < maxAge {
			logx.Printf("%s cached %v ago, not requested", url, time.Since(info.ModTime()).Round(time.Second))
			return Cached{Data: cached}, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Cached{}, err
	}
	req.Header.Set("User-Agent", UserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cacheErr == nil {
		if etag, err := os.ReadFile(etagPath); err == nil {
			req.Header.Set("If-None-Match", strings.TrimSpace(string(etag)))
		}
	}
	fallback := func(err error) (Cached, error) {
		logx.Printf("%s failed: %v, cache available %v", url, err, cacheErr == nil)
		if ctx.Err() != nil {
			return Cached{}, ctx.Err()
		}
		if cacheErr == nil {
			return Cached{Data: cached, Stale: err}, nil
		}
		return Cached{}, err
	}
	resp, err := Client.Do(req)
	if err != nil {
		return fallback(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && cacheErr == nil {
		logx.Printf("%s not modified, using cache (%d bytes)", url, len(cached))
		now := time.Now()
		os.Chtimes(cachePath, now, now)
		return Cached{Data: cached}, nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fallback(err)
	}
	if resp.StatusCode != http.StatusOK {
		return fallback(&StatusError{URL: url, Status: resp.StatusCode, Body: truncate(data), Header: resp.Header})
	}
	if valid != nil && !valid(data) {
		return fallback(errors.New(url + " returned invalid data"))
	}
	logx.Printf("%s downloaded, %d bytes", url, len(data))
	if err := writeAtomic(cachePath, data); err == nil {
		if etag := resp.Header.Get("ETag"); etag != "" {
			writeAtomic(etagPath, []byte(etag))
		} else {
			os.Remove(etagPath)
		}
	}
	return Cached{Data: data}, nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
