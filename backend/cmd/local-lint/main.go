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

	args := []string{"run"}
	if *unusedOnly {
		args = append(args, "--enable-only=unused,staticcheck")
	}
	args = append(args, "./...")
	if err := run(linter, args, backendDir, nil); err != nil {
		exitError(err)
	}
	if *unusedOnly {
		fmt.Println("Go unused/staticcheck checks passed")
	} else {
		fmt.Println("Go lint checks passed")
	}
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
