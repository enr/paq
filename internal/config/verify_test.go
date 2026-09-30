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
		{"cosign complete", VerifyConfig{Cosign: CosignConfig{PublicKey: "k", Signature: "s"}}, true},
		{"cosign public key only", VerifyConfig{Cosign: CosignConfig{PublicKey: "k"}}, false},
		{"cosign signature only", VerifyConfig{Cosign: CosignConfig{Signature: "s"}}, false},
		{"cosign key bundle", VerifyConfig{Cosign: CosignConfig{PublicKey: "k", Bundle: "b"}}, true},
		{"cosign keyless bundle", VerifyConfig{Cosign: CosignConfig{Bundle: "b", CertificateOIDCIssuer: "i"}}, true},
		{"cosign keyless bundle without issuer", VerifyConfig{Cosign: CosignConfig{Bundle: "b"}}, false},
		{"github attestation", VerifyConfig{GitHubAttestation: &GitHubAttestationConfig{}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Enabled(); got != tc.want {
				t.Errorf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
