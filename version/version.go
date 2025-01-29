package version

import "github.com/janpfeifer/gonb/internal/version"

// AppVersion contains version and Git commit information.
//
// The placeholders are replaced on `git archive` using the `export-subst` attribute.
var AppVersion = version.AppVersion(GitTag, "$Format:%(describe)$", "$Format:%H$")
