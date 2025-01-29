package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

type VersionInfo struct {
	Version     string
	Commit      string
	CommitLink  string
	ReleaseLink string
}

const (
	BaseVersionControlURL string = "https://github.com/janpfeifer/gonb"
)

// AppVersion determines version and commit information based on multiple data sources:
//   - AppVersion information dynamically added by `git archive` in the remaining to parameters.
//   - A hardcoded version number passed as first parameter.
//   - Commit information added to the binary by `go build`.
//
// It's supposed to be called like this in combination with setting the `export-subst` attribute for the corresponding
// file in .gitattributes:
//
//	var AppVersion = version.AppVersion("1.0.0-rc1", "$Format:%(describe)$", "$Format:%H$")
//
// When exported using `git archive`, the placeholders are replaced in the file and this version information is
// preferred. Otherwise the hardcoded version is used and augmented with commit information from the build metadata.
//
// Source: https://github.com/Icinga/icingadb/blob/51068fff46364385f3c0165aab7b7393fa6a303b/pkg/version/version.go
func AppVersion(version, gitVersion, gitHash string) *VersionInfo {
	if !strings.HasPrefix(gitVersion, "$") && !strings.HasPrefix(gitHash, "$") {
		versionInfo := &VersionInfo{
			Version:     gitVersion,
			Commit:      gitHash,
			ReleaseLink: fmt.Sprintf("%s/release/%s", BaseVersionControlURL, gitVersion),
		}
		if len(gitHash) > 0 {
			versionInfo.CommitLink = fmt.Sprintf("%s/tree/%s", BaseVersionControlURL, gitHash)
		}

		return versionInfo
	} else {
		var commit string
		var releaseVersion string

		if info, ok := debug.ReadBuildInfo(); ok {
			modified := false

			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					commit = setting.Value
				case "vcs.modified":
					modified, _ = strconv.ParseBool(setting.Value)
				}
				if strings.Contains(setting.Key, "ldflags") &&
					strings.Contains(setting.Value, "git.tag") {

					start := strings.Index(setting.Value, "git.tag=") + 8
					end := strings.Index(setting.Value[start:], "'") + start
					version = setting.Value[start:end]
				}
			}

			// Same truncation length for the commit hash
			const hashLen = 7
			releaseVersion = version

			if len(commit) >= hashLen {
				if modified {
					version += "-dirty"
					commit += " (modified)"
				}
			}
		}

		versionInfo := &VersionInfo{
			Version:     version,
			Commit:      commit,
			ReleaseLink: fmt.Sprintf("%s/release/%s", BaseVersionControlURL, releaseVersion),
		}
		if len(commit) > 0 {
			versionInfo.CommitLink = fmt.Sprintf("%s/tree/%s", BaseVersionControlURL, commit)
		}

		return versionInfo
	}
}

// GetInfo Get version info
func (v *VersionInfo) GetInfo() VersionInfo {
	return *v
}

// String Get version as a string
func (v *VersionInfo) String() string {
	return v.Version
}

// Print writes verbose version output to stdout.
func (v *VersionInfo) Print() {
	fmt.Println("GoNB version:", v.Version)
	fmt.Println()

	if len(v.CommitLink) > 0 {
		fmt.Println("Version control info:")
		fmt.Printf("  Commit: %s \n", v.CommitLink)
		fmt.Printf("  Release: %s \n", v.ReleaseLink)
		fmt.Println()
	}

	fmt.Println("Build info:")
	fmt.Printf("  Go version: %s (OS: %s, arch: %s)\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func (v *VersionInfo) PrintMarkdown() string {
	var markdown string
	markdown += fmt.Sprintf("## GoNB version: `%s`\n\n", v.Version)

	if len(v.CommitLink) > 0 {
		markdown += "### Version Control Info\n"
		markdown += fmt.Sprintf("- Commit: [%s](%s)\n", v.Commit, v.CommitLink)
		markdown += fmt.Sprintf("- Release: [%s](%s)\n\n", v.Version, v.ReleaseLink)
	}

	markdown += "### Build Info\n"
	markdown += fmt.Sprintf("- Go version: %s (OS: %s, Arch: %s)\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	return markdown
}
