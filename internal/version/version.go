// Package version reports build metadata for the running binary.
//
// The package-level variables below are stamped at link time by the release
// pipeline:
//
//	go build -trimpath -ldflags "-s -w \
//	  -X github.com/JerryBian/splash/internal/version.version=v1.2.3 \
//	  -X github.com/JerryBian/splash/internal/version.commit=$(git rev-parse HEAD) \
//	  -X github.com/JerryBian/splash/internal/version.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// When they are left empty — a plain `go build` during development — the
// information the Go toolchain already records in the binary is used instead,
// so `splash --version` stays useful without any extra ceremony.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
)

// Values overridden with -ldflags "-X ...".
var (
	version string
	commit  string
	date    string
)

// Info describes the build that produced the running binary.
type Info struct {
	// Version is a semantic version such as v1.4.2, or "dev" for a build made
	// outside the release pipeline.
	Version string `json:"version"`
	// Commit is the full VCS revision the binary was built from.
	Commit string `json:"commit"`
	// Date is the build timestamp in RFC 3339, when known.
	Date string `json:"date,omitempty"`
	// GoVersion is the toolchain release, e.g. go1.27.4.
	GoVersion string `json:"goVersion"`
	// Platform is GOOS/GOARCH, e.g. linux/amd64.
	Platform string `json:"platform"`
	// Modified reports whether the build tree had uncommitted changes.
	Modified bool `json:"modified,omitempty"`
}

// Get returns the build information. The result is resolved once and cached.
func Get() Info { return resolve() }

var resolve = sync.OnceValue(func() Info {
	info := Info{
		Version:   version,
		Commit:    commit,
		Date:      date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	if build, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "" && build.Main.Version != "" && build.Main.Version != "(devel)" {
			info.Version = build.Main.Version
		}
		if build.GoVersion != "" {
			info.GoVersion = build.GoVersion
		}
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = setting.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = setting.Value
				}
			case "vcs.modified":
				info.Modified, _ = strconv.ParseBool(setting.Value)
			}
		}
	}

	if info.Version == "" {
		info.Version = "dev"
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	return info
})

// ShortCommit returns the first 7 characters of the commit hash, which is what
// humans want to read in a log line or a UI footer.
func (i Info) ShortCommit() string {
	if len(i.Commit) > 7 {
		return i.Commit[:7]
	}
	return i.Commit
}

// String renders a one-line summary, e.g. "v1.4.2 (a1b2c3d, 2026-11-21, go1.27.4)".
func (i Info) String() string {
	var b strings.Builder
	b.WriteString(i.Version)
	b.WriteString(" (")
	b.WriteString(i.ShortCommit())
	if i.Date != "" {
		b.WriteString(", ")
		b.WriteString(i.Date)
	}
	b.WriteString(", ")
	b.WriteString(i.GoVersion)
	b.WriteString(", ")
	b.WriteString(i.Platform)
	if i.Modified {
		b.WriteString(", modified")
	}
	b.WriteString(")")
	return b.String()
}

// UserAgent builds a value suitable for outbound HTTP calls, e.g.
// "splash/1.4.2 (+https://github.com/JerryBian/splash)".
func (i Info) UserAgent(repoURL string) string {
	if repoURL == "" {
		return fmt.Sprintf("splash/%s", i.Version)
	}
	return fmt.Sprintf("splash/%s (+%s)", i.Version, repoURL)
}
