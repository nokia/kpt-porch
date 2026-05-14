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

package functionconfigs

import (
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/kptdev/krm-functions-catalog/functions/go/apply-replacements/replacements"
	"github.com/kptdev/krm-functions-catalog/functions/go/set-namespace/transformer"
	"github.com/kptdev/krm-functions-catalog/functions/go/starlark/starlark"
	"github.com/kptdev/krm-functions-sdk/go/fn"
	configapi "github.com/nephio-project/porch/api/porchconfig/v1alpha1"
	imageutil "github.com/nephio-project/porch/pkg/util/image"
	pkgerrors "github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	LatestTag   = "latest"
	AllPrefixes = "*"
)

type InternalCacheEntry struct {
	Entry map[string]map[string]configapi.FunctionConfigSpec
	// objName stores what cluster object the entry was read from.
	// Used to check for conflicts/duplicate definitions.
	objName client.ObjectKey
}

type FunctionConfigStore struct {
	mu sync.RWMutex

	// image base name -> prefix -> tag
	internalCache map[string]InternalCacheEntry

	// processorMapping contains all the built-in functions that can be executed as a Go function
	processorMapping map[string]fn.ResourceListProcessorFunc

	defaultImagePrefix string
	defaultBinaryDir   string
}

func NewStore(defaultImagePrefix, defaultBinaryDir string) *FunctionConfigStore {
	procMap := map[string]fn.ResourceListProcessorFunc{
		"apply-replacements": replacements.ApplyReplacements,
		"set-namespace":      set_namespace.Run,
		"starlark":           starlark.Process,
	}

	return &FunctionConfigStore{
		defaultImagePrefix: strings.TrimRight(defaultImagePrefix, "/"),
		defaultBinaryDir:   strings.TrimRight(defaultBinaryDir, "/"),

		// processorMapping contains all the built-in functions that can be executed as a Go function
		processorMapping: procMap,

		internalCache: make(map[string]InternalCacheEntry),
	}
}

func (s *FunctionConfigStore) Store(obj *configapi.FunctionConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	spec := &obj.Spec

	objKey := client.ObjectKeyFromObject(obj)
	if entry, ok := s.internalCache[spec.Image]; ok && entry.objName != objKey {
		return apierrors.NewConflict(
			configapi.TypeFunctionConfig.GroupResource(),
			client.ObjectKeyFromObject(obj).String(),
			pkgerrors.Errorf("Image %q is already configured from object %q", spec.Image, objKey),
		)
	}

	// remove any unnecessary data that will only be used for map keys
	strippedSpec := *spec.DeepCopy()
	strippedSpec.Prefixes = nil
	if strippedSpec.PodExecutor != nil {
		strippedSpec.PodExecutor.Tags = nil
	}
	if strippedSpec.BinaryExecutor != nil {
		strippedSpec.BinaryExecutor.Tags = nil
	}
	if strippedSpec.GoExecutor != nil {
		strippedSpec.GoExecutor.Tags = nil
	}

	prefixes := s.normalizePrefixes(spec.Prefixes)

	// if no prefixes are given, assume the user wants to apply the config to all prefixes
	if len(prefixes) == 0 {
		prefixes = append(prefixes, AllPrefixes)
	}

	s.internalCache[spec.Image] = InternalCacheEntry{
		Entry:   make(map[string]map[string]configapi.FunctionConfigSpec),
		objName: objKey,
	}
	for _, prefix := range prefixes {
		// One tag can technically have multiple types of configurations,
		// but handling the overlap would be less efficient than just doing multiple writes.
		s.internalCache[spec.Image].Entry[prefix] = make(map[string]configapi.FunctionConfigSpec)
		if spec.GoExecutor != nil {
			for _, tag := range spec.GoExecutor.Tags {
				if tag == "" {
					s.internalCache[spec.Image].Entry[prefix][LatestTag] = strippedSpec
				} else if tag == LatestTag {
					s.internalCache[spec.Image].Entry[prefix][""] = strippedSpec
				}
				s.internalCache[spec.Image].Entry[prefix][tag] = strippedSpec
			}
		}
		if spec.BinaryExecutor != nil {
			for _, tag := range spec.BinaryExecutor.Tags {
				if strippedSpec.BinaryExecutor.Path[0] != '/' {
					var err error
					strippedSpec.BinaryExecutor.Path, err = filepath.Abs(filepath.Join(s.defaultBinaryDir, spec.BinaryExecutor.Path))
					if err != nil {
						klog.Warningf("Failed to cache %q: %v", spec.Image, err)
					}
				}
				if tag == "" {
					s.internalCache[spec.Image].Entry[prefix][LatestTag] = strippedSpec
				} else if tag == LatestTag {
					s.internalCache[spec.Image].Entry[prefix][""] = strippedSpec
				}
				s.internalCache[spec.Image].Entry[prefix][tag] = strippedSpec
			}
		}
		if spec.PodExecutor != nil {
			for _, tag := range spec.PodExecutor.Tags {
				if tag == "" {
					s.internalCache[spec.Image].Entry[prefix][LatestTag] = strippedSpec
				} else if tag == LatestTag {
					s.internalCache[spec.Image].Entry[prefix][""] = strippedSpec
				}
				s.internalCache[spec.Image].Entry[prefix][tag] = strippedSpec
			}
		}
	}

	return nil
}

// normalizePrefixes strips additional slashes, inlines the default image prefix and removes duplicates
func (s *FunctionConfigStore) normalizePrefixes(prefixes []string) []string {
	prefixesSet := make(map[string]struct{})
	for _, prefix := range prefixes {
		prefix = strings.Trim(prefix, "/")
		if prefix == "" {
			prefixesSet[s.defaultImagePrefix] = struct{}{}
		}
		if strings.Trim(prefix, "/") == s.defaultImagePrefix {
			prefixesSet[""] = struct{}{}
		}
		prefixesSet[prefix] = struct{}{}
	}

	return slices.Collect(maps.Keys(prefixesSet))
}

func (s *FunctionConfigStore) Delete(imageName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.internalCache, imageName)
}

func (s *FunctionConfigStore) DeleteByObjName(key client.ObjectKey) {
	toDelete := ""
	for imageName, entry := range s.internalCache {
		if entry.objName == key {
			toDelete = imageName
			break
		}
	}

	if toDelete != "" {
		s.Delete(toDelete)
	}
}

func (s *FunctionConfigStore) Get(fullImageName string) (configapi.FunctionConfigSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	parsedImage := imageutil.Parse(fullImageName)
	if imageEntry, ok := s.internalCache[parsedImage.BaseName]; ok {
		prefixEntry, ok := imageEntry.Entry[parsedImage.Prefix()]

		if !ok {
			prefixEntry, ok = imageEntry.Entry[AllPrefixes]
		}

		if ok {
			if tagEntry, ok := prefixEntry[parsedImage.Tag]; ok {
				return tagEntry, true
			}
		}
	}
	return configapi.FunctionConfigSpec{}, false
}

func (s *FunctionConfigStore) GetByConstraint(fullImageName, constraint string) (configapi.FunctionConfigSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	parsedImage := imageutil.Parse(fullImageName)
	if imageEntry, ok := s.internalCache[parsedImage.BaseName]; ok {
		prefixEntry, ok := imageEntry.Entry[parsedImage.Prefix()]

		if !ok {
			prefixEntry, ok = imageEntry.Entry[AllPrefixes]
		}

		if ok {
			tags := slices.Collect(maps.Keys(prefixEntry))
			best, err := imageutil.FindBestSemverMatch(constraint, tags)
			if err != nil {
				klog.Warningf("Failed to find best semantic version for image %q by constraint %q: %v", fullImageName, constraint, err)
				return configapi.FunctionConfigSpec{}, false
			}
			return prefixEntry[best], true
		}
	}
	return configapi.FunctionConfigSpec{}, false
}

// GetProcessor looks up a function processor by image, holding the read lock for the duration of the lookup.
func (s *FunctionConfigStore) GetProcessor(imageName string) (fn.ResourceListProcessor, bool) {
	config, ok := s.Get(imageName)
	if !ok {
		return nil, false
	}

	return s.getProcessorForConfig(&config)
}

func (s *FunctionConfigStore) GetProcessorByConstraint(imageName, constraint string) (fn.ResourceListProcessor, bool) {
	config, ok := s.GetByConstraint(imageName, constraint)
	if !ok {
		return nil, false
	}

	return s.getProcessorForConfig(&config)
}

func (s *FunctionConfigStore) getProcessorForConfig(config *configapi.FunctionConfigSpec) (fn.ResourceListProcessor, bool) {
	if config.GoExecutor == nil {
		return nil, false
	}

	if config.GoExecutor.ID != nil {
		proc, ok := s.processorMapping[*config.GoExecutor.ID]
		return proc, ok
	}

	proc, ok := s.processorMapping[config.Image]
	return proc, ok
}

func (s *FunctionConfigStore) SendWarmupRequest(image imageutil.ParsedImage) {

}

// TODO: this is atrociously inefficient
// TODO: only used by pod warmup, so fixing that will fix this
func (s *FunctionConfigStore) IterPodConfigs() iter.Seq2[string, configapi.PodExecutorConfig] {
	return func(yield func(string, configapi.PodExecutorConfig) bool) {
		s.mu.RLock()
		defer s.mu.RUnlock()

		for imageName, imageEntry := range s.internalCache {
			for _, prefixEntry := range imageEntry.Entry {
				for _, tagEntry := range prefixEntry {
					if tagEntry.PodExecutor != nil {
						if !yield(imageName, *tagEntry.PodExecutor) {
							return
						}
					}
				}
			}
		}
	}
}

func (s *FunctionConfigStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.internalCache)
}
