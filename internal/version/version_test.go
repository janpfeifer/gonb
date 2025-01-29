package version

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestAppVersion(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		gitDescribe string
		gitHash     string
		want        *VersionInfo
	}{
		{
			name:        "With git information",
			version:     "1.0.0",
			gitDescribe: "v1.0.0",
			gitHash:     "abc1234",
			want: &VersionInfo{
				Version:    "v1.0.0",
				Commit:     "abc1234",
				CommitLink: "https://github.com/janpfeifer/gonb/tree/abc1234",
			},
		},
		{
			name:        "Without git information",
			version:     "1.0.0",
			gitDescribe: "$Format:%(describe)$",
			gitHash:     "$Format:%H$",
			want: &VersionInfo{
				Version: "1.0.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppVersion(tt.version, tt.gitDescribe, tt.gitHash)

			if got.Version != tt.want.Version {
				t.Errorf("AppVersion().Version = %v, want %v", got.Version, tt.want.Version)
			}

			if got.Commit != tt.want.Commit {
				t.Errorf("AppVersion().Commit = %v, want %v", got.Commit, tt.want.Commit)
			}

			if got.CommitLink != tt.want.CommitLink {
				t.Errorf("AppVersion().VersionControlLink = %v, want %v", got.CommitLink, tt.want.CommitLink)
			}
		})
	}
}

func TestVersionInfo_GetInfo(t *testing.T) {
	v := &VersionInfo{
		Version:    "1.0.0",
		Commit:     "abc123",
		CommitLink: "https://github.com/janpfeifer/gonb/tree/abc123",
	}

	got := v.GetInfo()
	if got != *v {
		t.Errorf("VersionInfo.GetInfo() = %v, want %v", got, *v)
	}
}

func TestVersionInfo_String(t *testing.T) {
	v := &VersionInfo{
		Version:    "1.0.0",
		Commit:     "abc123",
		CommitLink: "https://github.com/janpfeifer/gonb/tree/abc123",
	}

	got := v.String()
	if got != v.Version {
		t.Errorf("VersionInfo.String() = %v, want %v", got, v.Version)
	}
}

func TestVersionInfo_Print(t *testing.T) {
	v := &VersionInfo{
		Version:    "1.0.0",
		Commit:     "abc123",
		CommitLink: "https://github.com/janpfeifer/gonb/tree/abc123",
	}

	// Capture output to verify it contains expected information
	output := captureOutput(func() {
		v.Print()
	})

	// Check if output contains expected information
	expectedStrings := []string{
		"GoNB version: 1.0.0",
		"Version control info:",
		v.CommitLink,
		"Build info:",
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Print() output missing expected string: %s", expected)
		}
	}
}

// Helper function to capture stdout dynamically
func captureOutput(f func()) string {
	// Redirect stdout to a buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Call the function
	f()

	// Restore stdout and read buffer
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}
