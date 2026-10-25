package figma

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func apiClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient("fixture-secret", "pat", time.Second, 0)
	client.baseURL = server.URL + "/v1"
	return client, server
}

func TestGetAuthenticationAndQuery(t *testing.T) {
	for _, auth := range []string{"pat", "oauth"} {
		t.Run(auth, func(t *testing.T) {
			client, _ := apiClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/files/abc/nodes" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if got := r.URL.Query().Get("ids"); got != "1:2,1:3" {
					t.Errorf("ids = %q", got)
				}
				pat, bearer := r.Header.Get("X-Figma-Token"), r.Header.Get("Authorization")
				if auth == "pat" && (pat != "fixture-secret" || bearer != "") {
					t.Error("wrong PAT headers")
				}
				if auth == "oauth" && (pat != "" || bearer != "Bearer fixture-secret") {
					t.Error("wrong OAuth headers")
				}
				io.WriteString(w, `{"nodes":{}}`)
			})
			client.auth = auth
			data, err := client.Get(context.Background(), "/files/abc/nodes", url.Values{"ids": {"1:2,1:3"}})
			if err != nil || string(data) != `{"nodes":{}}` {
				t.Fatalf("Get = %s, %v", data, err)
			}
		})
	}
}

func TestStatusErrorsDoNotExposeBodyOrToken(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, _ := apiClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(status)
				io.WriteString(w, "fixture-secret https://signed.invalid/?token=sensitive")
			})
			_, err := client.Get(context.Background(), "/files/abc", nil)
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != status {
				t.Fatalf("expected HTTP %d, got %v", status, err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("unsafe diagnostic: %v", err)
			}
		})
	}
}

func TestAuthenticatedRedirectIsNotFollowed(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
	}))
	defer destination.Close()
	client, _ := apiClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"?signed=secret", http.StatusFound)
	})
	_, err := client.Get(context.Background(), "/files/abc", nil)
	if err == nil || destinationCalls.Load() != 0 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("redirect followed or unsafe error: calls=%d err=%v", destinationCalls.Load(), err)
	}
}

func TestRetryAfterThenSuccess(t *testing.T) {
	var calls atomic.Int32
	client, _ := apiClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{}`)
	})
	client.retries = 1
	if _, err := client.Get(context.Background(), "/files/abc", nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("got %d attempts", calls.Load())
	}
}

func TestRetryAfterDoesNotRetryEarly(t *testing.T) {
	var calls atomic.Int32
	client, _ := apiClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{}`)
	})
	client.timeout = 3 * time.Second
	client.retries = 1
	start := time.Now()
	if _, err := client.Get(context.Background(), "/files/abc", nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < time.Second || calls.Load() != 2 {
		t.Fatalf("retried too early or wrong attempts: duration=%s calls=%d", time.Since(start), calls.Load())
	}
}

func TestRetryBoundAndDeadline(t *testing.T) {
	var calls atomic.Int32
	client, _ := apiClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	client.retries = 2
	if _, err := client.Get(context.Background(), "/files/abc", nil); err == nil || calls.Load() != 3 {
		t.Fatalf("retry bound: calls=%d err=%v", calls.Load(), err)
	}
	client, _ = apiClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	client.retries = 2
	_, err := client.Get(context.Background(), "/files/abc", nil)
	if err == nil || !strings.Contains(err.Error(), "1h0m0s") || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("long wait was not explained: %v", err)
	}
}

func TestCancellationInterruptsRetryWait(t *testing.T) {
	responded := make(chan struct{})
	client, _ := apiClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		close(responded)
	})
	client.timeout = 0
	client.retries = 1
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.Get(ctx, "/files/abc", nil)
		done <- err
	}()
	<-responded
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt wait")
	}
}

func TestRequestTimeoutAndReadTimeout(t *testing.T) {
	for _, flush := range []bool{false, true} {
		t.Run(map[bool]string{false: "headers", true: "body"}[flush], func(t *testing.T) {
			client, _ := apiClient(t, func(w http.ResponseWriter, r *http.Request) {
				if flush {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			})
			client.timeout = 20 * time.Millisecond
			_, err := client.Get(context.Background(), "/files/abc", nil)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected deadline, got %v", err)
			}
		})
	}
}

func TestRetryDelay(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		header  string
		attempt int
		want    time.Duration
	}{
		{"7", 0, 7 * time.Second},
		{"0", 0, 0},
		{now.Add(9 * time.Second).Format(http.TimeFormat), 0, 9 * time.Second},
		{now.Add(-time.Hour).Format(http.TimeFormat), 0, 0},
		{"", 0, time.Second},
		{"invalid", 3, 8 * time.Second},
		{"-1", 0, time.Second},
		{"9223372036854775807", 0, time.Duration(1<<63 - 1)},
		{"99999999999999999999999", 0, time.Duration(1<<63 - 1)},
	} {
		if got := retryDelay(tc.header, now, tc.attempt); got != tc.want {
			t.Errorf("retryDelay(%q, %d) = %v, want %v", tc.header, tc.attempt, got, tc.want)
		}
	}
}

func TestResponseSizeLimit(t *testing.T) {
	for _, declared := range []bool{true, false} {
		t.Run(map[bool]string{true: "content-length", false: "streamed"}[declared], func(t *testing.T) {
			client, _ := apiClient(t, func(w http.ResponseWriter, r *http.Request) {
				if declared {
					w.Header().Set("Content-Length", "67108865")
					return
				}
				w.(http.Flusher).Flush()
				_, _ = io.CopyN(w, zeroReader{}, maxJSONBytes+1)
			})
			client.timeout = 10 * time.Second
			_, err := client.Get(context.Background(), "/files/abc", nil)
			if err == nil || !strings.Contains(err.Error(), "64 MiB") {
				t.Fatalf("expected size limit, got %v", err)
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestDownloadWithoutCredentialsAndNoOverwrite(t *testing.T) {
	payload := []byte("fixture asset")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("X-Figma-Token") != "" || r.Header.Get("Authorization") != "" {
			t.Error("asset download sent credentials or used a mutation")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/asset?signed=fixture", http.StatusFound)
			return
		}
		w.Write(payload)
	}))
	defer server.Close()
	client := NewClient("fixture-secret", "pat", time.Second, 0)
	client.http.Transport = server.Client().Transport
	dir := t.TempDir()
	target := filepath.Join(dir, "export.png")
	if err := client.Download(context.Background(), server.URL+"/redirect", target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("downloaded %q, %v", got, err)
	}
	if err := client.Download(context.Background(), server.URL+"/asset", target); err == nil {
		t.Fatal("existing target was overwritten")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestDownloadRejectsUnsafeURLsAndRedirect(t *testing.T) {
	client := NewClient("fixture-secret", "pat", time.Second, 0)
	for _, source := range []string{"http://example.com/asset", "https://user:password@example.com/asset", "https://example.com/asset#fragment", ":bad"} {
		if err := client.Download(context.Background(), source, filepath.Join(t.TempDir(), "asset")); err == nil || strings.Contains(err.Error(), "password") {
			t.Errorf("unsafe URL handling for %q: %v", source, err)
		}
	}
	var unsafeCalls atomic.Int32
	unsafe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unsafeCalls.Add(1)
	}))
	defer unsafe.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, unsafe.URL+"?signed=secret", http.StatusFound)
	}))
	defer server.Close()
	client.http.Transport = server.Client().Transport
	err := client.Download(context.Background(), server.URL, filepath.Join(t.TempDir(), "asset"))
	if err == nil || unsafeCalls.Load() != 0 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe redirect: calls=%d err=%v", unsafeCalls.Load(), err)
	}
}

func TestDownloadFailureCleansTemporaryFile(t *testing.T) {
	for _, mode := range []string{"truncated", "oversized", "streamed oversized", "status"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "truncated":
					w.Header().Set("Content-Length", "20")
					io.WriteString(w, "short")
				case "oversized":
					w.Header().Set("Content-Length", "134217729")
				case "streamed oversized":
					w.(http.Flusher).Flush()
					_, _ = io.CopyN(w, zeroReader{}, maxAssetBytes+1)
				case "status":
					w.WriteHeader(http.StatusForbidden)
					io.WriteString(w, "fixture-secret")
				}
			}))
			defer server.Close()
			client := NewClient("fixture-secret", "pat", 10*time.Second, 0)
			client.http.Transport = server.Client().Transport
			dir := t.TempDir()
			err := client.Download(context.Background(), server.URL+"?signed=secret", filepath.Join(dir, "asset"))
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected safe failure, got %v", err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatalf("partial download left behind: %v", entries)
			}
		})
	}
}

func TestInvalidPathAndCredentialsDoNotMakeRequests(t *testing.T) {
	var calls atomic.Int32
	client, _ := apiClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, path := range []string{"https://example.com", "//example.com", "/files?a=b", "/files#fragment"} {
		if _, err := client.Get(context.Background(), path, nil); err == nil {
			t.Errorf("accepted invalid path %q", path)
		}
	}
	client.token = "secret\nheader"
	if _, err := client.Get(context.Background(), "/files/abc", nil); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("unsafe token error: %v", err)
	}
	client.token = ""
	if _, err := client.Get(context.Background(), "/files/abc", nil); err == nil {
		t.Error("accepted missing token")
	}
	client.auth = "invalid"
	if _, err := client.Get(context.Background(), "/files/abc", nil); err == nil {
		t.Error("accepted invalid auth")
	}
	if calls.Load() != 0 {
		t.Fatalf("made %d requests for invalid input", calls.Load())
	}
}
