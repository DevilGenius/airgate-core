package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const golangciLintVersion = "v2.12.2"

func main() {
	unusedOnly := flag.Bool("unused", false, "only run unused and staticcheck linters")
	flag.Parse()

	repoRoot, backendDir, err := findRepoDirs()
	if err != nil {
		exitError(err)
	}
	linter, err := ensureGolangciLint(repoRoot)
	if err != nil {
		exitError(err)
	}

	cmd, err := lintCommand(linter, repoRoot, backendDir, *unusedOnly)
	if err != nil {
		exitError(err)
	}
	fmt.Printf("Go lint state: %s (concurrent checks in this repo wait for the lock)\n", filepath.Join(repoRoot, ".tools", "golangci-lint"))
	if err := cmd.Run(); err != nil {
		exitError(err)
	}
	if *unusedOnly {
		fmt.Println("Go unused/staticcheck checks passed")
	} else {
		fmt.Println("Go lint checks passed")
	}
}

func lintCommand(linter, repoRoot, backendDir string, unusedOnly bool) (*exec.Cmd, error) {
	stateDir := filepath.Join(repoRoot, ".tools", "golangci-lint")
	tmpDir := filepath.Join(stateDir, "tmp")
	cacheDir := filepath.Join(stateDir, "cache")
	goCacheDir := filepath.Join(stateDir, "go-build")
	for _, dir := range []string{tmpDir, cacheDir, goCacheDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create lint state directory %s: %w", dir, err)
		}
	}

	args := []string{"run", "--allow-serial-runners"}
	if unusedOnly {
		args = append(args, "--enable-only=unused,staticcheck")
	}
	args = append(args, "./...")
	cmd := exec.Command(linter, args...)
	cmd.Dir = backendDir
	// golangci-lint uses os.TempDir()/golangci-lint.lock, not its cache directory,
	// for locking. Its go list subprocess also needs a writable Go build cache;
	// inaccessible shared cache entries otherwise surface as "no go files".
	// Isolate all three for the linter and its children, not the caller's Go env.
	cmd.Env = append(os.Environ(),
		"TMP="+tmpDir,
		"TEMP="+tmpDir,
		"TMPDIR="+tmpDir,
		"GOLANGCI_LINT_CACHE="+cacheDir,
		"GOCACHE="+goCacheDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, nil
}

func findRepoDirs() (string, string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	for {
		backendDir := filepath.Join(dir, "backend")
		if fileExists(filepath.Join(backendDir, "go.mod")) {
			return dir, backendDir, nil
		}
		if fileExists(filepath.Join(dir, "go.mod")) && filepath.Base(dir) == "backend" {
			return filepath.Dir(dir), dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", errors.New("could not find airgate-core backend/go.mod")
		}
		dir = parent
	}
}

func ensureGolangciLint(repoRoot string) (string, error) {
	toolsBin := filepath.Join(repoRoot, ".tools", "bin")
	if err := os.MkdirAll(toolsBin, 0o755); err != nil {
		return "", err
	}
	exe := filepath.Join(toolsBin, executableName("golangci-lint"))
	if linterVersionMatches(exe) {
		return exe, nil
	}
	fmt.Printf("Installing golangci-lint %s...\n", golangciLintVersion)
	env := append(os.Environ(),
		"GOBIN="+toolsBin,
		"GOTOOLCHAIN=local",
	)
	if err := run("go", []string{"install", "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@" + golangciLintVersion}, repoRoot, env); err != nil {
		return "", err
	}
	if !linterVersionMatches(exe) {
		return "", fmt.Errorf("installed golangci-lint does not report %s", strings.TrimPrefix(golangciLintVersion, "v"))
	}
	return exe, nil
}

func linterVersionMatches(path string) bool {
	if !fileExists(path) {
		return false
	}
	cmd := exec.Command(path, "version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false
	}
	return strings.Contains(out.String(), "version "+strings.TrimPrefix(golangciLintVersion, "v"))
}

func run(name string, args []string, dir string, env []string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
