package verify

import (
	"strings"
	"testing"
)

const testHash = "7583b9f5d300bd6ac882160417d95bbd0978ebef04fb195844243ebd9f3ebe1c"

func TestParseSHA256JSON(t *testing.T) {
	cases := []struct {
		name     string
		doc      string
		selector string
		want     string
	}{
		{
			name:     "top level key",
			doc:      `{"name":"1.136.2","sha256hash":"` + testHash + `"}`,
			selector: "sha256hash",
			want:     testHash,
		},
		{
			name:     "nested object",
			doc:      `{"dist":{"checksums":{"sha256":"` + testHash + `"}}}`,
			selector: "dist.checksums.sha256",
			want:     testHash,
		},
		{
			name:     "array index",
			doc:      `{"assets":[{"digest":"nope"},{"digest":"` + testHash + `"}]}`,
			selector: "assets.1.digest",
			want:     testHash,
		},
		{
			name:     "oci style prefix is stripped",
			doc:      `{"digest":"sha256:` + testHash + `"}`,
			selector: "digest",
			want:     testHash,
		},
		{
			name:     "sri style prefix is stripped",
			doc:      `{"digest":"sha256-` + testHash + `"}`,
			selector: "digest",
			want:     testHash,
		},
		{
			name:     "uppercase and surrounding space are normalized",
			doc:      `{"h":"  ` + strings.ToUpper(testHash) + `  "}`,
			selector: "h",
			want:     testHash,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempFile(t, "checksums.json", []byte(tc.doc))
			got, err := ParseSHA256JSON(path, tc.selector)
			if err != nil {
				t.Fatalf("ParseSHA256JSON: %v", err)
			}
			if got != tc.want {
				t.Errorf("hash = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestParseSHA256JSONErrors covers the ways a selector can miss: each must be
// reported as a checksum-file problem, never fall through as a valid hash.
func TestParseSHA256JSONErrors(t *testing.T) {
	cases := []struct {
		name     string
		doc      string
		selector string
	}{
		{"not json", `<html>404</html>`, "sha256hash"},
		{"empty selector", `{"h":"` + testHash + `"}`, ""},
		{"missing key", `{"h":"` + testHash + `"}`, "nope"},
		{"key inside a string", `{"h":"` + testHash + `"}`, "h.deeper"},
		{"index out of range", `{"a":[{"h":"x"}]}`, "a.3.h"},
		{"non numeric array index", `{"a":[{"h":"x"}]}`, "a.first.h"},
		{"value is not a string", `{"h":123}`, "h"},
		{"value is not a digest", `{"h":"not-a-hash"}`, "h"},
		{"digest of the wrong length", `{"h":"abc123"}`, "h"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempFile(t, "checksums.json", []byte(tc.doc))
			got, err := ParseSHA256JSON(path, tc.selector)
			if err == nil {
				t.Fatalf("expected an error, got hash %q", got)
			}
		})
	}
}

// TestRunSHA256JSONSelector verifies that Plan.SHA256Selector switches the
// checksum document from the coreutils layout to JSON.
func TestRunSHA256JSONSelector(t *testing.T) {
	artifact := writeTempFile(t, "artifact.bin", []byte("payload"))
	// sha256("payload")
	const payloadHash = "239f59ed55e737c77147cf55ad0c1b030b6d7ee748a7426952f9b852d5a935e5"
	doc := writeTempFile(t, "checksums.json", []byte(`{"sha256hash":"`+payloadHash+`"}`))

	plan := Plan{
		ArtifactPath:    artifact,
		ArtifactName:    "artifact.bin",
		SHA256AssetPath: doc,
		SHA256Selector:  "sha256hash",
	}
	if err := Run(plan); err != nil {
		t.Errorf("Run with a JSON checksum document: %v", err)
	}

	// A mismatching hash must still fail.
	bad := writeTempFile(t, "bad.json", []byte(`{"sha256hash":"`+testHash+`"}`))
	plan.SHA256AssetPath = bad
	if err := Run(plan); err == nil {
		t.Error("expected a mismatch error for a wrong hash, got nil")
	}
}
