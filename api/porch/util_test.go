// Copyright 2026 The kpt Authors
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

package porch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidSubpackageDir(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		expectValid bool
	}{
		// Invalid cases
		{name: "empty string", dir: "", expectValid: false},
		{name: "leading slash", dir: "/subpkg", expectValid: false},
		{name: "trailing slash", dir: "subpkg/", expectValid: false},
		{name: "double dots at start", dir: "../subpkg", expectValid: false},
		{name: "double dots in middle", dir: "sub/../pkg", expectValid: false},
		{name: "double dots at end", dir: "subpkg/..", expectValid: false},
		{name: "only double dots", dir: "..", expectValid: false},
		{name: "dot segment at start", dir: "./subpkg", expectValid: false},
		{name: "dot segment in middle", dir: "sub/./pkg", expectValid: false},
		{name: "only dot", dir: ".", expectValid: false},
		{name: "leading and trailing slash", dir: "/subpkg/", expectValid: false},
		{name: "spaces in path", dir: "sub pkg", expectValid: false},
		{name: "special characters", dir: "sub@pkg", expectValid: false},
		{name: "backslash", dir: "sub\\pkg", expectValid: false},
		{name: "colon in path", dir: "sub:pkg", expectValid: false},
		{name: "empty segment (double slash)", dir: "sub//pkg", expectValid: false},
		{name: "with underscores (invalid DNS)", dir: "my_subpkg", expectValid: false},
		{name: "mixed with underscores (invalid DNS)", dir: "my-sub_pkg.v1/nested-dir", expectValid: false},
		{name: "with dots in name", dir: "my.subpkg", expectValid: false},

		// Valid cases
		{name: "simple directory", dir: "subpkg", expectValid: true},
		{name: "nested directory", dir: "path/to/subpkg", expectValid: true},
		{name: "two levels", dir: "sub/pkg", expectValid: true},
		{name: "with hyphens", dir: "my-subpkg", expectValid: true},
		{name: "numeric name", dir: "123", expectValid: true},
		{name: "deeply nested", dir: "a/b/c/d/e", expectValid: true},
		{name: "single char segments", dir: "a/b/c", expectValid: true},
		{name: "starts with digit", dir: "1subpackage", expectValid: true},
		{name: "ends with digit", dir: "subpackage1", expectValid: true},
		{name: "contains digits", dir: "1subpckage2/3subpackage4/5subpackage6", expectValid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := IsValidSubpackageDir(tt.dir)
			if tt.expectValid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestComposeSubpkgObjName(t *testing.T) {
	subpackageName, err := ComposeSubpkgObjName("")
	assert.NotNil(t, err)
	assert.Equal(t, "", subpackageName)

	subpackageName, err = ComposeSubpkgObjName("my-subpackage")
	assert.Nil(t, err)
	assert.Equal(t, "my-subpackage", subpackageName)

	subpackageName, err = ComposeSubpkgObjName("level1/level2/my-subpackage")
	assert.Nil(t, err)
	assert.Equal(t, "level1.level2.my-subpackage", subpackageName)

	subpackageName, err = ComposeSubpkgObjName("/level1/level2/")
	assert.NotNil(t, err)
	assert.Equal(t, "", subpackageName)
}
