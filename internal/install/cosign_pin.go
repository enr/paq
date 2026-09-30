package install

// The cosign release paq downloads as its private copy when no cosign is
// available and a spec needs keyless signature verification.
//
// The hashes are pinned here, in code, rather than read from the cosign
// registry recipe: recipes can be overridden by the user's overlays or by a
// registry snapshot, and this is the trust anchor for every keyless check, so
// it must not depend on configuration. They come from the release's
// cosign_checksums.txt. Bumping the version means updating all of them.
const cosignPinVersion = "v3.1.3"

// cosignPinSHA256 maps "os/arch" to the sha256 of the release binary.
var cosignPinSHA256 = map[string]string{
	"linux/amd64":   "4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71",
	"linux/arm64":   "c5d324e091826b0d7a78eb16fef316450b4eb9aaec045611c08ba06f5e73220a",
	"darwin/amd64":  "2347488e5d5b25336644024dfeca5601b190e91197a71a917bda44744aff106c",
	"darwin/arm64":  "5cf948c2f4dfe59687bdd0b8523709067383e03982cc543475c8a7dc70e92a76",
	"windows/amd64": "9fe59be0eca1271873ce019061335eb1ac419b7059202e797828467ddabe33be",
}

// cosignReleaseBase is the download base of the cosign releases; a var so
// tests can point it at a local server.
var cosignReleaseBase = "https://github.com/sigstore/cosign/releases/download"

// cosignMinVersion is the oldest cosign whose verify-blob paq relies on
// (Sigstore bundle v0.3 without extra flags). An older cosign found on the
// system is skipped in favor of the next candidate.
const cosignMinVersion = "3.0.0"
