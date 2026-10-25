// Package figma provides a read-only client for the Figma REST API.
package figma

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	maxJSONBytes  int64 = 64 << 20
	maxAssetBytes int64 = 128 << 20
)

// Client sends only GET requests. Its credentials are never sent to asset hosts.
type Client struct {
	baseURL string
	http    *http.Client
	token   string
	auth    string
	timeout time.Duration
	retries int
}

// NewClient creates a client for a PAT (auth="pat") or OAuth access token
// (auth="oauth"). timeout bounds the whole Get or Download operation, including
// retries. A nonpositive timeout leaves the caller's context as the only deadline.
func NewClient(token, auth string, timeout time.Duration, retries int) *Client {
	return &Client{
		baseURL: "https://api.figma.com/v1",
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		token: token, auth: auth, timeout: timeout, retries: max(0, retries),
	}
}

// HTTPError deliberately excludes response bodies and URLs: either can contain
// credentials or signed download parameters.
type HTTPError struct {
	Operation  string
	StatusCode int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	message := fmt.Sprintf("%s: HTTP %d", e.Operation, e.StatusCode)
	switch e.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		message += " (check token validity, file_content:read scope and file access)"
	case http.StatusNotFound:
		message += " (file or resource not found)"
	case http.StatusTooManyRequests:
		message += fmt.Sprintf(" (rate limited; retry after %s)", e.RetryAfter)
	}
	return message
}

// Get reads an API response with a 64 MiB limit. The caller decodes the JSON.
// path is relative to /v1; query is encoded independently of path.
func (c *Client) Get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#") {
		return nil, errors.New("invalid Figma API path")
	}
	if c.auth != "pat" && c.auth != "oauth" {
		return nil, errors.New("authentication must be pat or oauth")
	}
	if c.token == "" || strings.ContainsAny(c.token, "\r\n") {
		return nil, errors.New("missing or invalid Figma token")
	}
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	response, err := c.get(ctx, c.http, endpoint, true, "Figma API")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.ContentLength > maxJSONBytes {
		return nil, errors.New("Figma API response exceeds 64 MiB limit; request fewer nodes or a smaller depth")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxJSONBytes+1))
	if err != nil {
		return nil, safeRequestError(ctx, "reading Figma API response", err)
	}
	if int64(len(data)) > maxJSONBytes {
		return nil, errors.New("Figma API response exceeds 64 MiB limit; request fewer nodes or a smaller depth")
	}
	return data, nil
}

// Download saves an HTTPS asset without credentials, with a 128 MiB limit.
// The target must not already exist. A temporary file in its directory is linked
// into place only after a complete, successful download, then removed.
func (c *Client) Download(ctx context.Context, source, target string) error {
	u, err := url.Parse(source)
	if err != nil || !validAssetURL(u) {
		return errors.New("asset URL must use HTTPS and contain no credentials")
	}
	if _, err := os.Lstat(target); err == nil {
		return errors.New("export target already exists; choose another path")
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot inspect export target")
	}
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	// Copy the transport and timeout, but never the authenticated redirect policy.
	// Asset redirects can change hosts; every destination must remain HTTPS.
	assetHTTP := *c.http
	assetHTTP.Jar = nil
	assetHTTP.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !validAssetURL(req.URL) {
			return errors.New("unsafe asset redirect")
		}
		req.Header.Del("X-Figma-Token")
		req.Header.Del("Authorization")
		return nil
	}
	response, err := c.get(ctx, &assetHTTP, source, false, "asset download")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.ContentLength > maxAssetBytes {
		return errors.New("asset exceeds 128 MiB limit")
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".figma-cli-*")
	if err != nil {
		return errors.New("cannot create temporary export file; check target directory")
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	defer temp.Close()
	n, err := io.Copy(temp, io.LimitReader(response.Body, maxAssetBytes+1))
	if err != nil {
		return safeRequestError(ctx, "saving asset", err)
	}
	if n > maxAssetBytes {
		return errors.New("asset exceeds 128 MiB limit")
	}
	if err := temp.Sync(); err != nil {
		return errors.New("cannot sync export file")
	}
	if err := temp.Close(); err != nil {
		return errors.New("cannot close export file")
	}
	// Link is atomic and refuses existing files, including racing writers and
	// symlinks. A rename would silently replace an existing target on Unix.
	if err := os.Link(tempName, target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("export target already exists; choose another path")
		}
		return errors.New("cannot publish export file; destination filesystem must support hard links")
	}
	return nil
}

func validAssetURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}

func (c *Client) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout > 0 {
		return context.WithTimeout(ctx, c.timeout)
	}
	return context.WithCancel(ctx)
}

func (c *Client) get(ctx context.Context, client *http.Client, endpoint string, authenticated bool, operation string) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, errors.New("cannot create HTTP request")
		}
		if authenticated {
			request.Header.Set("Accept", "application/json")
			if c.auth == "oauth" {
				request.Header.Set("Authorization", "Bearer "+c.token)
			} else {
				request.Header.Set("X-Figma-Token", c.token)
			}
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, safeRequestError(ctx, operation, err)
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		delay := retryDelay(response.Header.Get("Retry-After"), time.Now(), attempt)
		// Do not parse or expose server bodies, which can echo credentials.
		response.Body.Close()
		httpErr := &HTTPError{Operation: operation, StatusCode: response.StatusCode, RetryAfter: delay}
		if response.StatusCode != http.StatusTooManyRequests || attempt >= c.retries {
			return nil, httpErr
		}
		if deadline, ok := ctx.Deadline(); ok && delay >= time.Until(deadline) {
			return nil, fmt.Errorf("%w; retry wait exceeds remaining timeout", httpErr)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, safeRequestError(ctx, operation, ctx.Err())
		case <-timer.C:
		}
	}
}

func retryDelay(value string, now time.Time, attempt int) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		// Saturate rather than overflow a large Retry-After into a negative wait.
		if seconds > int64((time.Duration(1<<63-1))/time.Second) {
			return time.Duration(1<<63 - 1)
		}
		return time.Duration(seconds) * time.Second
	}
	if value != "" && strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		// Decimal values larger than int64 are still a request to wait, not a
		// malformed header that should fall back to an earlier retry.
		return time.Duration(1<<63 - 1)
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(0, date.Sub(now))
	}
	// A malformed/missing header gets a conservative bounded fallback.
	return time.Second * time.Duration(1<<min(attempt, 5))
}

func safeRequestError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", operation, ctx.Err())
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%s: request timed out", operation)
	}
	return fmt.Errorf("%s failed (network, TLS or I/O error)", operation)
}
