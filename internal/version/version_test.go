package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetIsPopulated(t *testing.T) {
	info := Get()

	// Outside the release pipeline the version is either "dev" or the module
	// version the toolchain recorded; what matters is that it is never empty.
	if info.Version == "" {
		t.Error("Version is empty")
	}
	if info.Commit == "" {
		t.Error("Commit is empty; it should fall back to \"unknown\"")
	}
	if info.GoVersion != runtime.Version() && !strings.HasPrefix(info.GoVersion, "go") {
		t.Errorf("GoVersion = %q", info.GoVersion)
	}
	want := runtime.GOOS + "/" + runtime.GOARCH
	if info.Platform != want {
		t.Errorf("Platform = %q, want %q", info.Platform, want)
	}
}

func TestGetIsCached(t *testing.T) {
	first, second := Get(), Get()
	if first != second {
		t.Errorf("Get() is not stable: %+v then %+v", first, second)
	}
}

func TestShortCommit(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"abc", "abc"},
		{"abcdefg", "abcdefg"},
		{"abcdefgh", "abcdefg"},
		{"a1b2c3d4e5f6a7b8c9d0", "a1b2c3d"},
	}
	for _, tt := range tests {
		info := Info{Commit: tt.in}
		if got := info.ShortCommit(); got != tt.want {
			t.Errorf("ShortCommit(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestInfoString(t *testing.T) {
	info := Info{
		Version:   "v1.2.3",
		Commit:    "a1b2c3d4e5f6",
		Date:      "2026-11-21T12:00:00Z",
		GoVersion: "go1.27.4",
		Platform:  "linux/amd64",
	}
	got := info.String()

	want := "v1.2.3 (a1b2c3d, 2026-11-21T12:00:00Z, go1.27.4, linux/amd64)"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestInfoStringNotesADirtyTree(t *testing.T) {
	info := Info{Version: "dev", Commit: "abc", GoVersion: "go1.27.4", Platform: "linux/amd64", Modified: true}
	if !strings.Contains(info.String(), "modified") {
		t.Errorf("String() = %q, want it to flag an uncommitted build", info.String())
	}
}

func TestInfoStringWithoutADate(t *testing.T) {
	info := Info{Version: "dev", Commit: "abc", GoVersion: "go1.27.4", Platform: "darwin/arm64"}
	if strings.Contains(info.String(), ", ,") || strings.Contains(info.String(), "(,") {
		t.Errorf("String() = %q has an empty date slot", info.String())
	}
}

func TestUserAgent(t *testing.T) {
	info := Info{Version: "v1.2.3"}

	if got, want := info.UserAgent(""), "splash/v1.2.3"; got != want {
		t.Errorf("UserAgent(\"\") = %q, want %q", got, want)
	}
	if got, want := info.UserAgent(RepoURL), "splash/v1.2.3 (+"+RepoURL+")"; got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}

func TestRepoURLIsAbsolute(t *testing.T) {
	if !strings.HasPrefix(RepoURL, "https://") {
		t.Errorf("RepoURL = %q, want an absolute https URL", RepoURL)
	}
	if !strings.HasSuffix(RepoURL, "/splash") {
		t.Errorf("RepoURL = %q, want it to point at this repository", RepoURL)
	}
}
