package osv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestQueryBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var req batchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Queries) != 2 || req.Queries[0].Package.Ecosystem != "npm" ||
			req.Queries[0].Package.Name != "esbuild" || req.Queries[0].Version != "0.24.0" {
			t.Errorf("unexpected queries: %+v", req.Queries)
		}
		w.Write([]byte(`{"results":[{"vulns":[{"id":"GHSA-67mh-4wv8-2f99","modified":"2025-01-01T00:00:00Z"}]},{}]}`))
	}))
	defer srv.Close()
	old := BatchURL
	BatchURL = srv.URL
	defer func() { BatchURL = old }()

	got, err := QueryBatch(context.Background(), nil, []Query{
		{Ecosystem: "npm", Package: "esbuild", Version: "0.24.0"},
		{Ecosystem: "crates.io", Package: "ripgrep", Version: "14.1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"GHSA-67mh-4wv8-2f99"}, {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestQueryBatchErrors(t *testing.T) {
	cases := map[string]func(w http.ResponseWriter){
		"http error":       func(w http.ResponseWriter) { http.Error(w, "boom", http.StatusInternalServerError) },
		"result mismatch":  func(w http.ResponseWriter) { w.Write([]byte(`{"results":[]}`)) },
		"malformed answer": func(w http.ResponseWriter) { w.Write([]byte(`{`)) },
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler(w) }))
			defer srv.Close()
			old := BatchURL
			BatchURL = srv.URL
			defer func() { BatchURL = old }()

			if _, err := QueryBatch(context.Background(), nil, []Query{{Ecosystem: "npm", Package: "x", Version: "1.0.0"}}); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestQueryBatchEmpty(t *testing.T) {
	old := BatchURL
	BatchURL = "http://127.0.0.1:0" // unreachable: no request must be made
	defer func() { BatchURL = old }()
	got, err := QueryBatch(context.Background(), nil, nil)
	if err != nil || got != nil {
		t.Errorf("got %v, %v; want nil, nil", got, err)
	}
}
