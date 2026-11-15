// Package version reports the build version of the running binary.
package version

import "runtime/debug"

// Version is injected at build time:
//
//	go build -ldflags "-X github.com/maksimkurb/cublyverse/internal/version.Version=1.2.3"
var Version string

// String returns the build version. When no version was injected it falls back
// to the VCS revision recorded in the build info, and finally to "dev".
func String() string {
	if Version != "" {
		return Version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var revision string
	var dirty bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if len(revision) >= 7 {
		short := revision[:7]
		if dirty {
			short += "-dirty"
		}
		return short
	}
	return "dev"
}
