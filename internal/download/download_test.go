package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestToTemp(t *testing.T) {
	content := make([]byte, 1000)
	for i := range content {
		content[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	var callbackCalled int
	progress := func(downloaded, total int64) {
		callbackCalled++
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
