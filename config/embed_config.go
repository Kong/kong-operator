// Package config provides functions to dump kustomize config YAMLs to a directory.
// Those files are used in test suites that can be imported in other repositories.
// In such case resolved (during runtime) path ./config points to root of that other
// repository instead of directory ./config in this this project, so it breaks
// everything. This package embeds config directory as Go code (embed.FS) and allow
// dumping it to a temp dir during runtime to have path that can be passed to e.g.
// kubectl.
package config

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed *
var configYAMLs embed.FS

// DumpKustomizeConfigToTempDir saves kustomize (directory ./config from KGO OSS repository)
// YAMLs used for testing to a temp dir and returns the path to the dir to be used with kubectl.
// Use returned cleaner function (only when no error is returned) to remove the directory.
func DumpKustomizeConfigToTempDir() (path string, cleaner func(), err error) {
	path, err = os.MkdirTemp("/tmp", "config")
	if err != nil {
		return "", nil, err
	}
	cleaner = func() {
		os.RemoveAll(path) // clean up
	}
	defer func() {
		if err != nil {
			cleaner()
		}
	}()
	if err := copyFS(path, configYAMLs); err != nil {
		return "", nil, err
	}
	return path, cleaner, err
}

// copyFS allows saving to a temporary directory content of ./config.
// Copy-pasted from https://github.com/golang/go/issues/62484.
func copyFS(dir string, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, _ error) error {
		targ := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			if err := os.MkdirAll(targ, 0777); err != nil {
				return err
			}
			return nil
		}
		r, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer r.Close()
		info, err := r.Stat()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(targ, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666|info.Mode()&0777)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, r); err != nil {
			w.Close()
			return fmt.Errorf("copying %s: %w", path, err)
		}
		if err := w.Close(); err != nil {
			return err
		}
		return nil
	})
}
