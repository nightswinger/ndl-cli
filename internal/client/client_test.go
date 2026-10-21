package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(srv *httptest.Server, interval time.Duration) *Client {
	return New(Config{
		BaseURL:    srv.URL,
		Interval:   interval,
		Backoff:    time.Millisecond,
		MaxRetries: 3,
		UserAgent:  UserAgent("test"),
	})
}

func TestUserAgentAndQuery(t *testing.T) {
	var ua, q string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.UserAgent()
		q = r.URL.Query().Get("query")
		w.Write([]byte("<ok/>"))
	}))
	defer srv.Close()

	c := newTestClient(srv, 0)
	body, err := c.Get(context.Background(), "/sru", url.Values{"query": {`title="桜"`}})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "<ok/>" {
		t.Errorf("body = %q", body)
	}
	if !strings.HasPrefix(ua, "ndl-cli/test (+https://") {
		t.Errorf("User-Agent = %q", ua)
	}
	if q != `title="桜"` {
		t.Errorf("query = %q", q)
	}
}

func TestRetryOn5xx(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv, 0).Get(context.Background(), "/x", nil); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("attempts = %d, want 3", n)
	}
}

func TestRetryGivesUpAfterMax(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestClient(srv, 0).Get(context.Background(), "/x", nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != 503 {
		t.Fatalf("err = %v", err)
	}
	if n != 4 {
		t.Errorf("attempts = %d, want 4 (1 + 3 retries)", n)
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("<bad/>"))
	}))
	defer srv.Close()

	_, err := newTestClient(srv, 0).Get(context.Background(), "/x", nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != 400 || string(he.Body) != "<bad/>" {
		t.Fatalf("err = %v", err)
	}
	if n != 1 {
		t.Errorf("attempts = %d, want 1", n)
	}
}

func TestRetryOn429WithMessage(t *testing.T) {
	body, err := os.ReadFile("../../testdata/error_429.xml")
	if err != nil {
		t.Fatal(err)
	}
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write(body)
	}))
	defer srv.Close()

	_, err = newTestClient(srv, 0).Get(context.Background(), "/x", nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != 429 {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(he.Message, "同時アクセス数") {
		t.Errorf("message = %q", he.Message)
	}
	if n != 4 {
		t.Errorf("attempts = %d, want 4", n)
	}
}

func TestNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // nothing listens any more

	c := New(Config{BaseURL: srv.URL, Backoff: time.Millisecond, MaxRetries: 1})
	_, err := c.Get(context.Background(), "/x", nil)
	var ne *NetworkError
	if !errors.As(err, &ne) {
		t.Fatalf("err = %T %v, want *NetworkError", err, err)
	}
}

func TestInterval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	c := newTestClient(srv, 80*time.Millisecond)
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background(), "/x", nil); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el < 160*time.Millisecond {
		t.Errorf("3 requests took %s, want >= 160ms with 80ms interval", el)
	}
}

func TestContextCancelDuringWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	c := newTestClient(srv, time.Hour)
	if _, err := c.Get(context.Background(), "/x", nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx, "/x", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}
