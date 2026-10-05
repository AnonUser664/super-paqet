// File version.go: reports executable build information so qualification and rollout can
// identify their binary.

package version

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	// Version records the release label supplied by the build process.
	Version = "v1.0.0-alpha.21"
	// GitCommit records the source revision so a running binary can be matched to qualification
	// evidence.
	GitCommit = "unknown"
	// GitTag records the release tag independently of a dirty source checkout.
	GitTag = "unknown"
	// BuildTime records build provenance for deployment/debug comparison.
	BuildTime = "unknown"
	// GoVersion records the toolchain identity used to interpret build/runtime compatibility.
	GoVersion = runtime.Version()
)

// Cmd registers the version command, including its options and explicit error propagation.
var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Prints the version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Version:    %s\n", Version)
		fmt.Printf("Git Tag:    %s\n", GitTag)
		fmt.Printf("Git Commit: %s\n", GitCommit)
		fmt.Printf("Build Time: %s\n", BuildTime)
		fmt.Printf("Go Version: %s\n", GoVersion)
		fmt.Printf("Platform:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}
