//go:build !windows

package plugin

import (
	"os"
	"path/filepath"
)

func replaceManifest(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	return syncGenerationDirectory(filepath.Dir(target))
}

func syncGenerationDirectory(path string) error {
	for _, name := range []string{path, filepath.Dir(path)} {
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		err = f.Sync()
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
