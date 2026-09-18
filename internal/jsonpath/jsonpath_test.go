package jsonpath

import (
	"encoding/json"
	"testing"
)

// decode mirrors how the callers feed this package: json.Unmarshal into an
// "any", then select. Using real JSON keeps the tests honest about the types
// the decoder actually produces (float64 for numbers, map[string]any, []any).
func decode(t *testing.T, doc string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("bad test fixture: %v", err)
	}
	return v
}

func TestSelectObjectKey(t *testing.T) {
	doc := decode(t, `{"sha256hash":"abc123"}`)
	got, err := Select(doc, "sha256hash")
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc123" {
		t.Errorf("Select = %v, want abc123", got)
	}
}

func TestSelectNestedKey(t *testing.T) {
	doc := decode(t, `{"build":{"digest":"deadbeef"}}`)
	got, err := Select(doc, "build.digest")
	if err != nil {
		t.Fatal(err)
	}
	if got != "deadbeef" {
		t.Errorf("Select = %v, want deadbeef", got)
	}
}

func TestSelectArrayIndex(t *testing.T) {
	doc := decode(t, `{"assets":[{"digest":"first"},{"digest":"second"}]}`)
	for _, tc := range []struct {
		selector string
		want     string
	}{
		{"assets.0.digest", "first"},
		{"assets.1.digest", "second"},
	} {
		got, err := Select(doc, tc.selector)
		if err != nil {
			t.Fatalf("Select(%q): %v", tc.selector, err)
		}
		if got != tc.want {
			t.Errorf("Select(%q) = %v, want %q", tc.selector, got, tc.want)
		}
	}
}

func TestSelectTopLevelArray(t *testing.T) {
	doc := decode(t, `[{"version":"1.2.3"}]`)
	got, err := Select(doc, "0.version")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.2.3" {
		t.Errorf("Select = %v, want 1.2.3", got)
	}
}

func TestSelectReturnsNonStringValues(t *testing.T) {
	doc := decode(t, `{"count":42,"nested":{"a":1}}`)

	got, err := Select(doc, "count")
	if err != nil {
		t.Fatal(err)
	}
	// encoding/json decodes every number into float64.
	if got != float64(42) {
		t.Errorf("Select(count) = %#v, want float64(42)", got)
	}

	// A selector may stop on an interior node.
	if _, err := Select(doc, "nested"); err != nil {
		t.Errorf("Select(nested): %v", err)
	}
}

// A selector that does not resolve must be an error and never a zero value:
// the caller turns the result into the expected checksum, so a silent "" would
// surface much later as a mismatch (or, worse, as a match against nothing).
func TestSelectErrors(t *testing.T) {
	doc := decode(t, `{"assets":[{"digest":"first"}],"name":"tool","nil":null}`)

	tests := []struct {
		name     string
		selector string
	}{
		{"empty selector", ""},
		{"missing key", "nope"},
		{"missing nested key", "assets.0.nope"},
		{"key on an array", "assets.digest"},
		{"index out of range", "assets.7.digest"},
		{"negative index", "assets.-1.digest"},
		{"descend into a string", "name.length"},
		{"descend into null", "nil.anything"},
		{"trailing empty segment", "name."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Select(doc, tc.selector)
			if err == nil {
				t.Fatalf("Select(%q) = %#v, want an error", tc.selector, got)
			}
			if got != nil {
				t.Errorf("Select(%q) returned %#v alongside an error, want nil", tc.selector, got)
			}
		})
	}
}

func TestString(t *testing.T) {
	doc := decode(t, `{"assets":[{"digest":"sha256:abc"}]}`)
	got, err := String(doc, "assets.0.digest")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sha256:abc" {
		t.Errorf("String = %q, want sha256:abc", got)
	}
}

// String must reject a non-string node rather than formatting it: a checksum
// document whose digest decodes to a number or an object is malformed input,
// not a digest.
func TestStringRejectsNonStringValues(t *testing.T) {
	doc := decode(t, `{"num":42,"obj":{},"arr":[],"bool":true,"nil":null}`)
	for _, selector := range []string{"num", "obj", "arr", "bool", "nil"} {
		t.Run(selector, func(t *testing.T) {
			got, err := String(doc, selector)
			if err == nil {
				t.Fatalf("String(%q) = %q, want an error", selector, got)
			}
			if got != "" {
				t.Errorf("String(%q) = %q alongside an error, want \"\"", selector, got)
			}
		})
	}
}

func TestStringPropagatesSelectError(t *testing.T) {
	doc := decode(t, `{"a":"b"}`)
	if _, err := String(doc, "missing"); err == nil {
		t.Fatal("expected the Select error to propagate")
	}
}
