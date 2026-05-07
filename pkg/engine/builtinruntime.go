// Copyright 2022, 2025-2026 The kpt and Nephio Authors
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

package engine

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	kptfilev1 "github.com/kptdev/kpt/pkg/api/kptfile/v1"
	"github.com/kptdev/kpt/pkg/fn"
	"github.com/kptdev/kpt/pkg/lib/kptops"
	fnsdk "github.com/kptdev/krm-functions-sdk/go/fn"
	"github.com/nephio-project/porch/controllers/functionconfigs/reconciler"
	regclientref "github.com/regclient/regclient/types/ref"
	"k8s.io/klog/v2"
)

type builtinRuntime struct {
	fnMapping map[string]fnsdk.ResourceListProcessor
}

func newBuiltinRuntime(functionConfigStore *reconciler.FunctionConfigStore) *builtinRuntime {
	return &builtinRuntime{
		fnMapping: functionConfigStore.GetExecCache(),
	}
}

var _ kptops.FunctionRuntime = &builtinRuntime{}

func (br *builtinRuntime) getRunnerByConstraint(funct *kptfilev1.Function) (fnsdk.ResourceListProcessor, error) {
	c, err := semver.NewConstraint(funct.Tag)
	if err != nil {
		return nil, &fn.NotFoundError{
			Function: kptfilev1.Function{Image: funct.Image},
		}
	}
	// Filter the cache map by semver constraint validation
	type candidate struct {
		baseName  string
		version   *semver.Version
		processor fnsdk.ResourceListProcessor
	}

	// Filter the cache map by semver constraint validation
	var filteredCache []candidate
	for img, proc := range br.fnMapping {
		// Extract the version string after ":v" in the image name
		idx := strings.LastIndex(img, ":v")
		// Currently, hashed and latest tagged images would be filtered out
		if idx == -1 {
			continue
		}

		baseName := img[:idx]
		if baseName != funct.Image {
			continue
		}

		versionStr := img[idx+1:] // skip past ":"
		v, err := semver.NewVersion(versionStr)
		if err != nil {
			klog.Infof("Failed to parse version %q from cached image %q: %v", versionStr, img, err)
			continue
		}

		cand := candidate{
			baseName:  baseName,
			version:   v,
			processor: proc,
		}
		if c.Check(v) {
			filteredCache = append(filteredCache, cand)
		}
	}

	// Check if any matching image was found
	if len(filteredCache) == 0 {
		klog.Infof("Image %q with constraint %q is not found in the cache", funct.Image, funct.Tag)
		return nil, &fn.NotFoundError{
			Function: kptfilev1.Function{Image: funct.Image},
		}
	}

	// Sort by semver and select the greatest version
	slices.SortFunc(filteredCache, func(a, b candidate) int {
		return a.version.Compare(b.version)
	})

	selected := filteredCache[len(filteredCache)-1]

	klog.Infof("Selected image \"%s:%s\" (version %s) for request %q",
		selected.baseName, selected.version.Original(), selected.version, funct.Image)

	return selected.processor, nil
}

func (br *builtinRuntime) GetRunner(ctx context.Context, funct *kptfilev1.Function) (fn.FunctionRunner, error) {
	builtinRunner := &builtinRunner{
		ctx: ctx,
	}

	if funct.Tag != "" {
		ref, err := regclientref.New(funct.Image)
		if err != nil {
			return nil, fmt.Errorf("failed to parse image %q as reference: %w", funct.Image, err)
		}
		// If the image already carries an inline tag, strip it
		// so filterByConstraint gets a bare repository name, and
		// we don't produce a double-tag
		if ref.Tag != "" {
			if stripped := strings.TrimSuffix(funct.Image, ":"+ref.Tag); stripped != funct.Image {
				klog.Infof("Image %q already contains tag %q; stripping it in favor of Tag constraint %q", funct.Image, ref.Tag, funct.Tag)
				funct.Image = stripped
			}
		}
		runner, err := br.getRunnerByConstraint(funct)
		if err != nil {
			return nil, err
		}
		builtinRunner.processor = runner
	} else {
		klog.Infof("Image tag is empty, using the image with explicit tag: %q", funct.Image)

		processor, found := br.fnMapping[funct.Image]
		if !found {
			klog.Infof("Image %q is not found in the cache", funct.Image)
			return nil, &fn.NotFoundError{Function: *funct}
		}
		builtinRunner.processor = processor
	}

	return builtinRunner, nil
}

func (br *builtinRuntime) Close() error {
	return nil
}

type builtinRunner struct {
	ctx       context.Context
	processor fnsdk.ResourceListProcessor
}

var _ fn.FunctionRunner = &builtinRunner{}

func (br *builtinRunner) Run(r io.Reader, w io.Writer) (err error) {
	// KRM functions often panic on input validation errors, so we need to convert panics to errors
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("KRM function panicked with: %v", p)
		}
	}()
	return fnsdk.Execute(br.processor, r, w)
}
