package main

import (
	"os"

	"github.com/janpfeifer/gonb/gonbui/protocol"
	"github.com/janpfeifer/gonb/version"
)

//go:generate bash -c "printf 'package main\nvar GitTag = \"%s\"\n' \"$(git describe --tags --abbrev=0)\" > version/versiontag.go"
//go:generate bash -c "printf 'package main\nvar GitCommitHash = \"%s\"\n' \"$(git rev-parse HEAD)\" > version/versionhash.go"

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func init() {
	gitCommit := version.AppVersion.Commit
	if gitCommit == "" {
		gitCommit = version.GitCommitHash
	}
	must(os.Setenv(protocol.GONB_GIT_COMMIT, gitCommit))
	must(os.Setenv(protocol.GONB_VERSION, version.AppVersion.Version))
}
