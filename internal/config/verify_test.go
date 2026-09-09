package config

import "testing"

func TestVerifyConfigEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  VerifyConfig
		want bool
	}{
		{"empty", VerifyConfig{}, false},
		{"sha256 literal", VerifyConfig{SHA256: "abc"}, true},
		{"sha256 asset", VerifyConfig{SHA256Asset: "{{asset}}.sha256"}, true},
		{"sha256 url", VerifyConfig{SHA256URL: "https://example.com/checksums.json"}, true},
		{"sha256 json alone", VerifyConfig{SHA256JSON: "sha256hash"}, false},
		{"sha512 literal", VerifyConfig{SHA512: "abc"}, true},
		{"sha512 asset", VerifyConfig{SHA512Asset: "{{asset}}.sha512"}, true},
		{"minisign complete", VerifyConfig{Minisign: MinisignConfig{PublicKey: "k", SignedAsset: "s"}}, true},
		{"minisign public key only", VerifyConfig{Minisign: MinisignConfig{PublicKey: "k"}}, false},
		{"minisign signed asset only", VerifyConfig{Minisign: MinisignConfig{SignedAsset: "s"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Enabled(); got != tc.want {
				t.Errorf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
