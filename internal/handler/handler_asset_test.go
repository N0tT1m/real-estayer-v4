package handler

import (
	"os"
	"path/filepath"
	"testing"
)

// AssetRoot decides where web/templates and web/static are found. Getting it
// wrong on Windows (where the binary is often launched from its own directory
// rather than the project root) previously meant a server that booted fine and
// then 500-ed every page.
func TestAssetRootPrefersEnvVar(t *testing.T) {
	t.Setenv("ASSETS_DIR", "/some/explicit/path")
	if got := AssetRoot(); got != "/some/explicit/path" {
		t.Errorf("AssetRoot() = %q, want the ASSETS_DIR value", got)
	}
}

func TestAssetRootUsesWorkingDirectoryWhenTemplatesPresent(t *testing.T) {
	t.Setenv("ASSETS_DIR", "")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "web", "templates", "layouts"), 0o750); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Chdir(dir)
	if got := AssetRoot(); got != "." {
		t.Errorf("AssetRoot() = %q, want \".\" when the CWD holds web/templates", got)
	}
}

func TestAssetRootFallsBackWhenNothingFound(t *testing.T) {
	t.Setenv("ASSETS_DIR", "")
	t.Chdir(t.TempDir())
	// No templates anywhere reachable: must still return something usable
	// rather than an empty string, so the error message names a real path.
	if got := AssetRoot(); got == "" {
		t.Error("AssetRoot() returned an empty path")
	}
}

func TestHasTemplateDir(t *testing.T) {
	dir := t.TempDir()
	if hasTemplateDir(dir) {
		t.Error("empty dir should not look like an asset root")
	}
	if err := os.MkdirAll(filepath.Join(dir, "web", "templates", "layouts"), 0o750); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !hasTemplateDir(dir) {
		t.Error("dir containing web/templates/layouts should be recognised")
	}
}
