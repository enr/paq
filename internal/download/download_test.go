package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/enr/paq/internal/httpretry"
)

func TestToTemp(t *testing.T) {
	// Larger than io.Copy's 32 KiB buffer on purpose, so the body arrives in
	// several reads: with a single read a progress counter that overwrites
	// instead of accumulating would be indistinguishable from a correct one.
	content := make([]byte, 100_000)
	for i := range content {
		content[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Declared explicitly: Go only infers Content-Length for small bodies,
		// and the announced total is what the progress bar scales against.
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	var callbackCalled int
	var lastDownloaded, lastTotal int64
	progress := func(downloaded, total int64) {
		callbackCalled++
		lastDownloaded, lastTotal = downloaded, total
	}

	path, err := ToTemp(context.Background(), srv.Client(), srv.URL+"/file", progress)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(content) {
		t.Errorf("downloaded %d bytes, want %d", len(data), len(content))
	}
	for i, b := range data {
		if b != content[i] {
			t.Errorf("byte %d = %d, want %d", i, b, content[i])
			break
		}
	}
	if callbackCalled == 0 {
		t.Error("progress callback was never called")
	}
	// The reported figures matter, not just the fact that the callback ran: a
	// counter that does not accumulate leaves the progress bar stuck at a few
	// bytes for the whole download.
	if lastDownloaded != int64(len(content)) {
		t.Errorf("last reported progress = %d bytes, want %d", lastDownloaded, len(content))
	}
	if lastTotal != int64(len(content)) {
		t.Errorf("reported total = %d, want %d (from Content-Length)", lastTotal, len(content))
	}
}

// TestToTempRejectsNon200 verifies that an error response is not written out as
// if it were the artifact. Without this, a stale asset URL makes paq save the
// 404 page, fail the checksum, and tell the user the file may have been
// tampered with — pointing at the wrong problem entirely.
func TestToTempRejectsNon200(t *testing.T) {
	// A 5xx is retried with a real backoff before failing: shrink it so the
	// test doesn't wait seconds for a decision it already knows.
	prevDelay := httpretry.BaseDelay
	httpretry.BaseDelay = time.Millisecond
	t.Cleanup(func() { httpretry.BaseDelay = prevDelay })

	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			// A dedicated TMPDIR makes the leftover check exact and keeps the
			// test independent of anything else writing to the shared temp dir.
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				w.Write([]byte("<html>error page</html>"))
			}))
			defer srv.Close()

			path, err := ToTemp(context.Background(), srv.Client(), srv.URL+"/asset.tar.gz", nil)
			if err == nil {
				os.Remove(path)
				t.Fatalf("expected an error for HTTP %d, got file %s", status, path)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(status)) {
				t.Errorf("error = %v, want it to report status %d", err, status)
			}

			entries, rerr := os.ReadDir(tmp)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if len(entries) != 0 {
				t.Errorf("temp dir holds %d leftover file(s) after a failed download", len(entries))
			}
		})
	}
}

func TestToTempRejectsNonHTTPScheme(t *testing.T) {
	_, err := ToTemp(context.Background(), http.DefaultClient, "file:///etc/passwd", nil)
	if err == nil {
		t.Fatal("expected an error for a non-http(s) scheme")
	}
}

func TestToTempLimitedRejectsOversizeWithContentLength(t *testing.T) {
	content := make([]byte, 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write(content)
	}))
	defer srv.Close()

	path, err := ToTempLimited(context.Background(), srv.Client(), srv.URL+"/file", 50, nil)
	if err == nil {
		os.Remove(path)
		t.Fatal("expected an error for a response exceeding maxBytes (declared via Content-Length)")
	}
}

func TestToTempLimitedRejectsOversizeWithoutContentLength(t *testing.T) {
	content := make([]byte, 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Chunked transfer: no Content-Length announced upfront.
		fw := &flushWriter{w: w}
		fw.Write(content)
	}))
	defer srv.Close()

	path, err := ToTempLimited(context.Background(), srv.Client(), srv.URL+"/file", 50, nil)
	if err == nil {
		os.Remove(path)
		t.Fatal("expected an error for a response exceeding maxBytes (no Content-Length)")
	}
}

func TestToTempLimitedAllowsWithinLimit(t *testing.T) {
	content := make([]byte, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	path, err := ToTempLimited(context.Background(), srv.Client(), srv.URL+"/file", 50, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(content) {
		t.Errorf("downloaded %d bytes, want %d", len(data), len(content))
	}
}

type flushWriter struct{ w http.ResponseWriter }

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if f, ok := fw.w.(http.Flusher); ok {
		f.Flush()
	}
	return n, err
}
