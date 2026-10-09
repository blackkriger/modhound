package github

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/blackkriger/modhound/internal/httpx"
)

func TestExplain(t *testing.T) {
	limited := &httpx.StatusError{Status: 403, Body: `{"message":"API rate limit exceeded"}`, Header: http.Header{
		"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Limit": {"60"}, "X-Ratelimit-Reset": {"1791260676"},
	}}
	err := explain(limited)
	if !strings.HasPrefix(err.Error(), "GitHub HTTP 403: the limit of 60 requests per hour is used up, it resets at ") || !errors.Is(err, limited) || httpx.Short(err) != "HTTP 403" {
		t.Errorf("rate limit: %v", err)
	}
	other := &httpx.StatusError{Status: 403, Body: `{"message":"You have exceeded a secondary rate limit."}`, Header: http.Header{}}
	if err := explain(other); err.Error() != "GitHub HTTP 403: You have exceeded a secondary rate limit." {
		t.Errorf("message: %v", err)
	}
	if err := explain(&httpx.StatusError{Status: 502, Body: "<html>", Header: http.Header{}}); err.Error() != "GitHub HTTP 502" {
		t.Errorf("plain: %v", err)
	}
	if explain(nil) != nil {
		t.Error("nil error must stay nil")
	}
}
