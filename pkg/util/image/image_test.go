// Copyright 2026 The kpt and Nephio Authors
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

package image

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindBestSemverMatch(t *testing.T) {
	cacheKeys := []string{
		"ghcr.io/kptdev/krm-functions-catalog/set-namespace:v0.4.1",
		"ghcr.io/kptdev/krm-functions-catalog/set-namespace:v0.4",
		"ghcr.io/kptdev/krm-functions-catalog/set-namespace@sha256:abcdef123456",
		"ghcr.io/kptdev/krm-functions-catalog/apply-replacements:v0.1.1",
		"ghcr.io/kptdev/krm-functions-catalog/apply-replacements:v0.1",
		"ghcr.io/kptdev/krm-functions-catalog/starlark:v0.4.3",
		"set-namespace:v0.4.1",
	}

	t.Run("selects highest matching version", func(t *testing.T) {
		cacheKeys := []string{
			"v0.4.1",
			"v0.4",
			"@sha256:abcdef123456",
		}
		key, err := FindBestSemverMatch(
			">= 0.4.0 < 0.5.0",
			"ghcr.io/kptdev/krm-functions-catalog/set-namespace",
			cacheKeys,
		)
		assert.NoError(t, err)
		assert.Equal(t, "v0.4.1", key)
	})

	t.Run("exact version match", func(t *testing.T) {
		cacheKeys := []string{
			"v0.1.1",
			"v0.1",
		}
		key, err := FindBestSemverMatch(
			"0.1.1",
			"ghcr.io/kptdev/krm-functions-catalog/apply-replacements",
			cacheKeys,
		)
		assert.NoError(t, err)
		assert.Equal(t, "v0.1.1", key)
	})

	t.Run("no matching version for valid constraint", func(t *testing.T) {
		_, err := FindBestSemverMatch(
			"> 1.0.0",
			"ghcr.io/kptdev/krm-functions-catalog/set-namespace",
			cacheKeys,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no image matching")
	})

	t.Run("image not in cache", func(t *testing.T) {
		_, err := FindBestSemverMatch(
			">= 0.1.0",
			"ghcr.io/kptdev/krm-functions-catalog/nonexistent",
			cacheKeys,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no image matching")
	})

	t.Run("invalid semver constraint", func(t *testing.T) {
		_, err := FindBestSemverMatch(
			">> 1.0.0",
			"ghcr.io/kptdev/krm-functions-catalog/set-namespace",
			cacheKeys,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid semver constraint")
	})

	t.Run("skips sha256-tagged entries", func(t *testing.T) {
		cacheKeys := []string{
			"v0.4.1",
			"v0.4",
			"@sha256:abcdef123456",
		}
		key, err := FindBestSemverMatch(
			">= 0.4.0",
			"ghcr.io/kptdev/krm-functions-catalog/set-namespace",
			cacheKeys,
		)
		assert.NoError(t, err)
		assert.NotContains(t, key, "@sha256:")
	})

	t.Run("matches without registry prefix", func(t *testing.T) {
		cacheKeys := []string{
			"v0.4.1",
			"v0.4",
			"@sha256:abcdef123456",
		}
		key, err := FindBestSemverMatch(
			">= 0.4.0",
			"set-namespace",
			cacheKeys,
		)
		assert.NoError(t, err)
		assert.Equal(t, "v0.4.1", key)
	})

	t.Run("empty cache keys", func(t *testing.T) {
		_, err := FindBestSemverMatch(
			">= 0.1.0",
			"ghcr.io/kptdev/krm-functions-catalog/set-namespace",
			[]string{},
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no image matching")
	})

	t.Run("selects greatest from multiple matches", func(t *testing.T) {
		keys := []string{
			"v1.0.0",
			"v1.1.0",
			"v1.2.0",
			"v2.0.0",
		}
		key, err := FindBestSemverMatch(">= 1.0.0 < 2.0.0", "myimage", keys)
		assert.NoError(t, err)
		assert.Equal(t, "v1.2.0", key)
	})

	t.Run("skips entries with unparseable versions", func(t *testing.T) {
		keys := []string{
			"v1.0.0",
			"vnotaversion",
		}
		key, err := FindBestSemverMatch(">= 1.0.0", "myimage", keys)
		assert.NoError(t, err)
		assert.Equal(t, "v1.0.0", key)
	})
}

func TestImageParse(t *testing.T) {
	const (
		registry         = "ghcr.io"
		registryWithPort = "my-registry.com:5000"
		subpath          = "kptdev/krm-functions-catalog"
		image            = "apply-setters"
		tag              = "v0.2.3"
		digest           = "sha256:7d89a74f106241391f687fc2985c8e6de597bb21f0d0014def5edc730618d9cc"
	)

	testCases := map[string]struct {
		input        string
		want         ParsedImage
	}{
		"empty": {
			input: "",
			want:  ParsedImage{},
		},
		"base name only": {
			input: image,
			want: ParsedImage{
				Original: image,
				BaseName: image,
			},
		},
		"tag only": {
			input: fmt.Sprintf("%s:%s", image, tag),
			want: ParsedImage{
				Original: fmt.Sprintf("%s:%s", image, tag),
				BaseName: image,
				Tag:      tag,
			},
		},
		"registry no path": {
			input: fmt.Sprintf("%s/%s", registry, image),
			want: ParsedImage{
				Original: fmt.Sprintf("%s/%s", registry, image),
				Registry: registry,
				BaseName: image,
			},
		},
		"registry with path": {
			input: fmt.Sprintf("%s/%s:%s", subpath, image, tag),
			want: ParsedImage{
				Original: fmt.Sprintf("%s/%s:%s", subpath, image, tag),
				SubPath:  subpath,
				BaseName: image,
				Tag:      tag,
			},
		},
		"fully qualified, no digest": {
			input: fmt.Sprintf("%s/%s/%s:%s", registry, subpath, image, tag),
			want: ParsedImage{
				Original: fmt.Sprintf("%s/%s/%s:%s", registry, subpath, image, tag),
				Registry: registry,
				SubPath:  subpath,
				BaseName: image,
				Tag:      tag,
			},
		},
		"digest without tag": {
			input: fmt.Sprintf("%s@%s", image, digest),
			want: ParsedImage{
				Original: fmt.Sprintf("%s@%s", image, digest),
				BaseName: image,
				Digest:   digest,
			},
		},
		"tag and digest": {
			input: fmt.Sprintf("%s:%s@%s", image, tag, digest),
			want: ParsedImage{
				Original: fmt.Sprintf("%s:%s@%s", image, tag, digest),
				BaseName: image,
				Tag:      tag,
				Digest:   digest,
			},
		},
		"registry with port": {
			input: fmt.Sprintf("%s/%s/%s:%s", registryWithPort, subpath, image, tag),
			want: ParsedImage{
				Original: fmt.Sprintf("%s/%s/%s:%s", registryWithPort, subpath, image, tag),
				Registry: registryWithPort,
				SubPath:  subpath,
				BaseName: image,
				Tag:      tag,
			},
		},
		"digest with nested repository": {
			input: fmt.Sprintf("%s/%s/%s@%s", registry, subpath, image, digest),
			want: ParsedImage{
				Original: fmt.Sprintf("%s/%s/%s@%s", registry, subpath, image, digest),
				Registry: registry,
				SubPath:  subpath,
				BaseName: image,
				Digest:   digest,
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			got := Parse(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}
