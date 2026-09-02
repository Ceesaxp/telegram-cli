// Package version is what a binary says it is.
//
// One package rather than a `version` variable in each of the three mains.
// Three variables means three -ldflags to remember and three chances to
// forget one, and a build where two binaries agree and the third says
// "dev" is a build nobody can report a bug against.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version is stamped at build time:
//
//	-ldflags "-X github.com/imtaqin/telegram-cli/internal/version.Version=v0.4.2"
//
// "dev" when nobody stamped it, which is the honest answer for a `go build`
// or a `go run` — see String for where it goes to look next.
var Version = "dev"

// Commit is the revision the build came from, stamped the same way. Empty
// when nobody said, which is usual: the module's build info carries it for
// free on any build from a git checkout.
var Commit = ""

// Asked reports whether the command line is asking for the version and
// nothing else.
//
// Checked before any flag parsing, in every binary, because two of the
// three take a subcommand as their first argument and would treat
// "version" as one — and because -version has to answer on a machine where
// the config is broken, which is one of the times somebody is most likely
// to be asking what they are running.
func Asked(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "version", "-version", "--version":
		return true
	}
	return false
}

// String is the one line a binary prints for -version.
//
//	tele-tui v0.4.2 (a1b2c3d, go1.25, darwin/arm64)
//
// The platform is in it because half the bug reports that matter are about
// a terminal on a platform, and asking afterwards costs a round trip with
// somebody who has already moved on.
func String(program string) string {
	out := program + " " + Version
	details := []string{}

	if commit := revision(); commit != "" {
		details = append(details, commit)
	}
	details = append(details, runtime.Version(),
		fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))

	return out + " (" + join(details) + ")"
}

// revision is the commit this binary was built from: the stamped one, or
// the one the toolchain recorded on its own.
//
// Go records vcs.revision in the build info for any build from a checkout
// with a clean working tree, and marks vcs.modified when it is not — so a
// binary built from uncommitted work says so rather than claiming to be the
// commit it started from.
func revision() string {
	if Commit != "" {
		return short(Commit)
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	commit, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			commit = short(setting.Value)
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if commit != "" && modified {
		commit += "-dirty"
	}
	return commit
}

// short is a revision at the length people actually quote.
func short(commit string) string {
	const shortLen = 7
	if len(commit) > shortLen {
		return commit[:shortLen]
	}
	return commit
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
