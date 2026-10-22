package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/noisefloor-oss/sdlc/internal/sdlc"
)

// Report only public runtime/build identifiers, never the full build settings
// (which can contain operator-supplied flags or environment-derived values).
func emitRuntimeVersion() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	file, err := os.Open(executable)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return err
	}
	result := struct {
		SchemaVersion    int    `json:"schemaVersion"`
		Version          string `json:"version"`
		Executable       string `json:"executable"`
		ExecutableDigest string `json:"executableDigest"`
		GoVersion        string `json:"goVersion"`
		VCSRevision      string `json:"vcsRevision,omitempty"`
		VCSModified      string `json:"vcsModified,omitempty"`
	}{
		SchemaVersion: 1, Version: sdlc.Version, Executable: executable,
		ExecutableDigest: fmt.Sprintf("sha256:%x", digest.Sum(nil)), GoVersion: runtime.Version(),
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				result.VCSRevision = setting.Value
			case "vcs.modified":
				result.VCSModified = setting.Value
			}
		}
	}
	return emit(result)
}
