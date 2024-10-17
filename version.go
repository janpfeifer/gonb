package main

import (
	"github.com/janpfeifer/gonb/gonbui/protocol"
	"os"
)

//go:generate bash -c "printf 'package main\nvar GitTag = \"%s\"\n' \"$(git describe --tags --abbrev=0)\" > versiontag.go"
//go:generate bash -c "printf 'package main\nvar GitCommitHash = \"%s\"\n' \"$(git rev-parse HEAD)\" > versionhash.go"

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func init() {
	must(os.Setenv(protocol.GONB_GIT_COMMIT, GitCommitHash))
	must(os.Setenv(protocol.GONB_VERSION, GitTag))
}
