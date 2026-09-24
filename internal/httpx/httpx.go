package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/blackkriger/modhound/internal/logx"
)

var UserAgent = "modhound (github.com/blackkriger/modhound)"

var transport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	MaxIdleConnsPerHost:   8,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
}

var Client = &http.Client{Timeout: 90 * time.Second, Transport: transport}

var downloadClient = &http.Client{Transport: transport}

const stallTimeout = time.Minute

type StatusError struct {
	URL    string
	Status int
	Body   string
}

func Short(err error) string {
	var se *StatusError
	if errors.As(err, &se) {
		return fmt.Sprintf("HTTP %d", se.Status)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return "no connection"
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: HTTP %d %s", e.URL, e.Status, e.Body)
}

func Do(ctx context.Context, method, url string, headers map[string]string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			if rd != nil {
				rd.(*bytes.Reader).Seek(0, io.SeekStart)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, url, rd)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		began := time.Now()
		resp, err := Client.Do(req)
		if err != nil {
			logx.Printf("http %s %s failed after %v (attempt %d): %v", method, url, time.Since(began).Round(time.Millisecond), attempt+1, err)
			lastErr = err
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		logx.Printf("http %s %s -> %d, %d bytes in %v (attempt %d)", method, url, resp.StatusCode, len(data), time.Since(began).Round(time.Millisecond), attempt+1)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = &StatusError{URL: url, Status: resp.StatusCode, Body: truncate(data)}
			if wait, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && wait > 0 && wait <= 30 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(wait) * time.Second):
				}
			}
			continue
		}
		if resp.StatusCode >= 300 {
			return &StatusError{URL: url, Status: resp.StatusCode, Body: truncate(data)}
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(data, out)
	}
	return lastErr
}

type progressWriter struct {
	w          io.Writer
	done, size int64
	fn         func(done, size int64)
	stall      *time.Timer
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.stall.Reset(stallTimeout)
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.fn != nil {
		p.fn(p.done, p.size)
	}
	return n, err
}

var ErrStalled = errors.New("download stalled")

func Download(ctx context.Context, url string, w io.Writer, progress func(done, size int64)) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stall := time.AfterFunc(stallTimeout, func() { cancel(ErrStalled) })
	defer stall.Stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/octet-stream")
	began := time.Now()
	resp, err := downloadClient.Do(req)
	if err != nil {
		err = stallCause(ctx, err)
		logx.Printf("download %s failed: %v", url, err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		logx.Printf("download %s -> %d", url, resp.StatusCode)
		return &StatusError{URL: url, Status: resp.StatusCode, Body: truncate(b)}
	}
	n, err := io.Copy(&progressWriter{w: w, size: resp.ContentLength, fn: progress, stall: stall}, resp.Body)
	err = stallCause(ctx, err)
	logx.Printf("download %s -> %d, %d of %d bytes in %v, err=%v", url, resp.StatusCode, n, resp.ContentLength, time.Since(began).Round(time.Millisecond), err)
	return err
}

func stallCause(ctx context.Context, err error) error {
	if err != nil && errors.Is(context.Cause(ctx), ErrStalled) {
		return ErrStalled
	}
	return err
}

func truncate(b []byte) string {
	if len(b) > 300 {
		b = b[:300]
	}
	return string(b)
}
