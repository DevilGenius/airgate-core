package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintCommandLoadsPackagesWithUnusableSharedState(t *testing.T) {
	repoRoot := t.TempDir()
	backendDir := filepath.Join(repoRoot, "backend")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"go.mod":   "module example.com/lintprobe\n\ngo 1.25.0\n",
		"probe.go": "package lintprobe\n\nconst Value = 1\n",
	} {
		if err := os.WriteFile(filepath.Join(backendDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// A regular file cannot be used as a temp/cache directory. This reproduces
	// unusable inherited state without relying on host-specific Windows ACLs.
	unusable := filepath.Join(repoRoot, "unusable-shared-state")
	if err := os.WriteFile(unusable, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stateKeys := []string{"TMP", "TEMP", "TMPDIR", "GOLANGCI_LINT_CACHE", "GOCACHE"}
	for _, key := range stateKeys {
		t.Setenv(key, unusable)
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOTOOLCHAIN", "local")

	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	linter, err := lintCommand(goPath, repoRoot, backendDir, true)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise actual package loading and export generation using exactly the
	// working directory and environment inherited by the linter's go subprocess.
	cmd := exec.Command(goPath, "list", "-export", "-json", "./...")
	cmd.Dir, cmd.Env = linter.Dir, linter.Env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed with isolated lint state: %v\n%s", err, output)
	}
	var pkg struct {
		ImportPath string
		GoFiles    []string
		Export     string
	}
	if err := json.Unmarshal(output, &pkg); err != nil {
		t.Fatalf("decode go list output: %v\n%s", err, output)
	}
	if pkg.ImportPath != "example.com/lintprobe" || len(pkg.GoFiles) != 1 {
		t.Fatalf("package sources were not loaded: %+v", pkg)
	}
	cacheDir := filepath.Join(repoRoot, ".tools", "golangci-lint", "go-build")
	rel, err := filepath.Rel(cacheDir, pkg.Export)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("export %q is outside local Go cache %q: %v", pkg.Export, cacheDir, err)
	}
	if _, err := os.Stat(pkg.Export); err != nil {
		t.Fatalf("missing compiled package export: %v", err)
	}
	for _, key := range stateKeys {
		if os.Getenv(key) != unusable {
			t.Errorf("lint setup changed the caller's %s", key)
		}
	}
}
