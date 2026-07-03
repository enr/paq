package registry

// DefaultPublicKey is the minisign public key that signs the official paq
// registry checksum file (base64, the one-line format of minisign.pub).
// It is the trust anchor for "paq registry update" from the default source.
//
// The matching secret key lives in the GitHub Actions secret
// MINISIGN_SECRET_KEY and is never committed. An empty value means this build
// has no signing key configured, so an update from the default source must
// refuse to run (a custom [registry] source supplies its own public key).
const DefaultPublicKey = ""
