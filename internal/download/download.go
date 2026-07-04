package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// ProgressFn is a callback invoked during the download with the bytes downloaded and the total.
// total is -1 if the server did not provide a Content-Length.
type ProgressFn func(downloaded, total int64)

// NewClient returns an *http.Client suitable for file downloads.
// It does not set an overall Timeout so large files are not cut off;
// connection-level timeouts come from http.DefaultTransport, and the
// caller's context handles cancellation. Centralizing construction here
// allows future tuning in one place.
func NewClient() *http.Client {
	return &http.Client{}
}

// ToTemp downloads url into a temp file and returns the file's path.
// The caller is responsible for removing the temp file after use.
// progress can be nil.
func ToTemp(ctx context.Context, client *http.Client, url string, progress ProgressFn) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", req.URL.Scheme)
	}

	// GitHub release assets are downloaded via the API asset endpoint
	// (api.github.com/.../releases/assets/{id}) with Accept: octet-stream and,
	// for private repos, the token. GitHub responds with a redirect to a signed
	// URL; the Go client strips the Authorization header on the host change, so
	// the token is not exposed to the destination storage.
	if req.URL.Host == "api.github.com" {
		req.Header.Set("Accept", "application/octet-stream")
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	total := resp.ContentLength // -1 if unknown

	tmp, err := os.CreateTemp("", "paq-download-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	var src io.Reader = resp.Body
	if progress != nil {
		src = &progressReader{r: resp.Body, total: total, fn: progress}
	}

	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write temp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	return tmpPath, nil
}

type progressReader struct {
	r          io.Reader
	total      int64
	downloaded int64
	fn         ProgressFn
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.downloaded += int64(n)
	if n > 0 && pr.fn != nil {
		pr.fn(pr.downloaded, pr.total)
	}
	return n, err
}
