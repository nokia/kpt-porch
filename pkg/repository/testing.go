// Copyright 2022-2026 The kpt Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package repository

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func ReadPackage(t *testing.T, packageDir string) PackageResources {
	results := map[string]string{}

	if err := filepath.WalkDir(packageDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// d.Type() may return 0 (unknown) on some filesystems;
		// fall back to Stat via d.Info() to avoid skipping regular files.
		if !d.Type().IsRegular() {
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("irregular file object detected: %q (%s)", p, info.Mode())
			}
		}
		rel, err := filepath.Rel(packageDir, p)
		if err != nil {
			return fmt.Errorf("failed to get relative path from %q to %q: %w", packageDir, p, err)
		}
		contents, err := os.ReadFile(p) // #nosec G304
		if err != nil {
			return fmt.Errorf("failed to open the source file %q: %w", p, err)
		}
		results[rel] = string(contents)
		return nil
	}); err != nil {
		t.Errorf("Failed to read package from disk %q: %v", packageDir, err)
	}
	return PackageResources{
		Contents: results,
	}
}

func WritePackage(t *testing.T, packageDir string, contents PackageResources) {
	for k, v := range contents.Contents {
		abs := filepath.Join(packageDir, k)
		dir := filepath.Dir(abs)
		// #nosec G301
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to crete directory %q: %v", dir, err)
		}
		// #nosec G306
		if err := os.WriteFile(abs, []byte(v), 0644); err != nil {
			t.Errorf("Failed to write package file %q: %v", abs, err)
		}
	}
}
