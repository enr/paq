package ghtoken

import "testing"

func TestFor(t *testing.T) {
	t.Cleanup(func() { byRepo = map[string]string{} })
	t.Setenv("GITHUB_TOKEN", "global")
	Set("Org/Private", "own")

	if got := For("org/private"); got != "own" {
		t.Errorf("For(org/private) = %q, want %q", got, "own")
	}
	if got := For("other/repo"); got != "global" {
		t.Errorf("For(other/repo) = %q, want %q", got, "global")
	}
}

func TestRepoFrom(t *testing.T) {
	cases := map[string]string{
		"https://api.github.com/repos/o/r/releases/tags/v1":                     "o/r",
		"GitHub API returned 404 for https://api.github.com/repos/o/r/releases": "o/r",
		"download https://github.com/o/r/releases/download/v1/x.zip: HTTP 404":  "o/r",
		"https://example.com/x.zip":                                             "",
		"https://github.com/o":                                                  "",
	}
	for in, want := range cases {
		if got := RepoFrom(in); got != want {
			t.Errorf("RepoFrom(%q) = %q, want %q", in, got, want)
		}
	}
}
