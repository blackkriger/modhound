package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestCachedGet(t *testing.T) {
	status := http.StatusOK
	var sawETag string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawETag = r.Header.Get("If-None-Match")
		switch {
		case status != http.StatusOK:
			w.WriteHeader(status)
			w.Write([]byte(`{"message":"no"}`))
		case sawETag == `"v1"`:
			w.WriteHeader(http.StatusNotModified)
		default:
			w.Header().Set("ETag", `"v1"`)
			w.Write([]byte(`[1]`))
		}
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "c.json")
	ctx := context.Background()

	res, err := CachedGet(ctx, srv.URL, path, 0, nil, nil)
	if err != nil || string(res.Data) != "[1]" || res.Stale != nil {
		t.Fatalf("first get: %q %v %v", res.Data, res.Stale, err)
	}
	res, err = CachedGet(ctx, srv.URL, path, 0, nil, nil)
	if err != nil || sawETag != `"v1"` || string(res.Data) != "[1]" || res.Stale != nil {
		t.Fatalf("conditional get: etag %q, %q %v %v", sawETag, res.Data, res.Stale, err)
	}
	status = http.StatusForbidden
	res, err = CachedGet(ctx, srv.URL, path, 0, nil, nil)
	var se *StatusError
	if err != nil || string(res.Data) != "[1]" || !errors.As(res.Stale, &se) || se.Status != http.StatusForbidden {
		t.Fatalf("stale get: %q %v %v", res.Data, res.Stale, err)
	}
	if _, err := CachedGet(ctx, srv.URL, filepath.Join(t.TempDir(), "none.json"), 0, nil, nil); !errors.As(err, &se) {
		t.Fatalf("no cache: want status error, got %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := CachedGet(cancelled, srv.URL, path, 0, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: want context.Canceled, got %v", err)
	}
	sawETag = "unset"
	res, err = CachedGet(ctx, srv.URL, path, time.Hour, nil, nil)
	if err != nil || sawETag != "unset" || string(res.Data) != "[1]" || res.Stale != nil {
		t.Fatalf("fresh cache must not be requested: etag %q, %q %v %v", sawETag, res.Data, res.Stale, err)
	}
}
