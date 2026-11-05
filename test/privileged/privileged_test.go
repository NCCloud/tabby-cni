// Package privileged runs the tests that need root and change the host network
// (bridges, vlans, routes, iptables and ebtables rules) inside a throwaway
// privileged container, so they can run on any machine with Docker without
// touching the host network.
//
// It is a separate Go module so testcontainers and its dependencies do not end
// up in the tabby-cni binary.
package privileged

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/mod/modfile"
)

// privilegedEnv matches internal/testutil.PrivilegedEnv in the main module.
const privilegedEnv = "TABBY_PRIVILEGED_TESTS"

func TestPrivileged(t *testing.T) {
	if os.Getenv(privilegedEnv) == "1" {
		t.Skip("already running inside the privileged container")
	}
	if testing.Short() {
		t.Skip("skipping privileged tests in short mode")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	root := repoRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	goVersion := goVersion(t, root)

	ctr, err := testcontainers.Run(ctx, "",
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{
			Context:   filepath.Join(root, "test", "privileged"),
			BuildArgs: map[string]*string{"GO_VERSION": &goVersion},
			Repo:      "tabby-cni-privileged-test",
			Tag:       goVersion,
			KeepImage: true,
		}),
		testcontainers.WithEnv(map[string]string{privilegedEnv: "1"}),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.Privileged = true
			hc.Binds = append(hc.Binds,
				root+":/src:ro",
				// Keep the module and build caches between runs.
				"tabby-cni-test-gomod:/go/pkg/mod",
				"tabby-cni-test-gocache:/root/.cache/go-build",
			)
		}),
		testcontainers.WithConfigModifier(func(c *container.Config) {
			c.WorkingDir = "/src"
		}),
		testcontainers.WithCmd("go", "test", "-count=1", "-v",
			// TestAPIs needs envtest and does not change the host network.
			"-skip", "^TestAPIs$",
			"./controllers/...", "./pkg/..."),
		// The default 60s wait deadline is too short when modules are not cached yet.
		testcontainers.WithWaitStrategyAndDeadline(15*time.Minute, wait.ForExit()),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("failed to run the privileged test container: %v", err)
	}

	logs, err := ctr.Logs(ctx)
	if err != nil {
		t.Fatalf("failed to read container logs: %v", err)
	}
	defer logs.Close()

	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		t.Log(scanner.Text())
	}

	state, err := ctr.State(ctx)
	if err != nil {
		t.Fatalf("failed to get container state: %v", err)
	}
	if state.ExitCode != 0 {
		t.Fatalf("privileged tests failed with exit code %d", state.ExitCode)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to find the test file location")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// goVersion returns the Go version from go.mod, so the container builds with
// the same Go as the project.
func goVersion(t *testing.T, root string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("failed to read go.mod: %v", err)
	}
	f, err := modfile.ParseLax("go.mod", data, nil)
	if err != nil {
		t.Fatalf("failed to parse go.mod: %v", err)
	}
	if f.Go == nil {
		t.Fatal("go.mod has no go directive")
	}
	return strings.TrimSpace(f.Go.Version)
}
