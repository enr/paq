package version

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArchLinuxProviderResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/packages/search/json/" || r.URL.Query().Get("name") != "ripgrep" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"pkgname":"ripgrep","pkgver":"14.1.1","pkgrel":"1","repo":"extra"}]}`))
	}))
	defer srv.Close()

	client := &http.Client{Transport: &prefixRoundTripper{base: srv.URL, inner: http.DefaultTransport}}
	p := ArchLinuxProvider{Pkg: "ripgrep", HTTPClient: client}

	ver, tag, err := p.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver != "14.1.1" {
		t.Errorf("version = %q, want 14.1.1", ver)
	}
	if tag != "14.1.1" {
		t.Errorf("tag = %q, want 14.1.1", tag)
	}
}

func TestArchLinuxProviderNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	client := &http.Client{Transport: &prefixRoundTripper{base: srv.URL, inner: http.DefaultTransport}}
	p := ArchLinuxProvider{Pkg: "does-not-exist", HTTPClient: client}

	if _, _, err := p.Resolve(context.Background()); err == nil {
		t.Fatal("expected error for empty results, got nil")
	}
}

func TestArchLinuxProviderHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &prefixRoundTripper{base: srv.URL, inner: http.DefaultTransport}}
	p := ArchLinuxProvider{Pkg: "ripgrep", HTTPClient: client}

	if _, _, err := p.Resolve(context.Background()); err == nil {
		t.Fatal("expected error for non-2xx status, got nil")
	}
}

func TestArchLinuxProviderEmptyPkg(t *testing.T) {
	p := ArchLinuxProvider{Pkg: ""}
	if _, _, err := p.Resolve(context.Background()); err == nil {
		t.Fatal("expected error for empty package name, got nil")
	}
}
