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

package reconciler

import (
	"context"
	"iter"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/kptdev/krm-functions-catalog/functions/go/apply-replacements/replacements"
	setNamespace "github.com/kptdev/krm-functions-catalog/functions/go/set-namespace/transformer"
	"github.com/kptdev/krm-functions-catalog/functions/go/starlark/starlark"
	fnsdk "github.com/kptdev/krm-functions-sdk/go/fn"
	configapi "github.com/nephio-project/porch/api/porchconfig/v1alpha1"
	imageutil "github.com/nephio-project/porch/pkg/util/image"
	pkgerrors "github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	BaseFinalizer           = "config.porch.kpt.dev/functionconfig"
	ServerFinalizer         = BaseFinalizer + "-porch-server"
	FunctionRunnerFinalizer = BaseFinalizer + "-function-runner"
	ControllerFinalizer     = BaseFinalizer + "-controller"

	AllPrefixes = "*"
)

// processorMapping contains all the built-in functions that can be executed as a Go function
//
// TODO: should this be inside the reconciler object?
var processorMapping = map[string]fnsdk.ResourceListProcessorFunc{
	"apply-replacements": replacements.ApplyReplacements,
	"set-namespace":      setNamespace.Run,
	"starlark":           starlark.Process,
}

type BinaryCacheEntry struct {
	PrefixRegex string
	Tags        map[string]string
}

type BuiltInCacheEntry struct {
	PrefixRegex string
	Process     fnsdk.ResourceListProcessor
	Tags        []string
}

type InternalCacheEntry struct {
	Entry map[string]map[string]configapi.FunctionConfigSpec
	// objName stores what cluster object the entry was read from.
	// Used to check for conflicts/duplicate definitions.
	objName types.NamespacedName
}

type FunctionConfigStore struct {
	mu sync.RWMutex

	// image base name -> prefix -> tag
	internalCache map[string]InternalCacheEntry

	defaultImagePrefix string
	defaultBinaryDir   string
}

func NewStore(defaultImagePrefix, defaultBinaryDir string) *FunctionConfigStore {
	return &FunctionConfigStore{
		defaultImagePrefix: strings.TrimRight(defaultImagePrefix, "/"),
		defaultBinaryDir:   strings.TrimRight(defaultBinaryDir, "/"),

		internalCache: make(map[string]InternalCacheEntry),
	}
}

func (s *FunctionConfigStore) Store(obj *configapi.FunctionConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	objKey := client.ObjectKeyFromObject(obj)
	if entry, ok := s.internalCache[obj.Spec.Image]; ok && entry.objName != objKey {
		return apierrors.NewConflict(
			configapi.TypeFunctionConfig.GroupResource(),
			client.ObjectKeyFromObject(obj).String(),
			pkgerrors.Errorf("Image %q is already configured from object %q", obj.Spec.Image, objKey),
		)
	}

	// remove any unnecessary data that will only be used for map keys
	strippedSpec := *obj.Spec.DeepCopy()
	strippedSpec.Prefixes = nil
	strippedSpec.PodExecutor.Tags = nil
	strippedSpec.BinaryExecutor.Tags = nil
	strippedSpec.GoExecutor.Tags = nil

	prefixes := s.normalizePrefixes(obj.Spec.Prefixes)

	// if no prefixes are given, assume the user wants to apply the config to all prefixes
	if len(prefixes) == 0 {
		prefixes = append(prefixes, AllPrefixes)
	}

	for _, prefix := range prefixes {
		// One tag can technically have multiple types of configurations,
		// but handling the overlap would be less efficient than just doing multiple writes.
		s.internalCache[obj.Spec.Image].Entry[prefix] = make(map[string]configapi.FunctionConfigSpec)
		for _, tag := range obj.Spec.GoExecutor.Tags {
			s.internalCache[obj.Spec.Image].Entry[prefix][tag] = strippedSpec
		}
		for _, tag := range obj.Spec.BinaryExecutor.Tags {
			s.internalCache[obj.Spec.Image].Entry[prefix][tag] = strippedSpec
		}
		for _, tag := range obj.Spec.PodExecutor.Tags {
			s.internalCache[obj.Spec.Image].Entry[prefix][tag] = strippedSpec
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
			prefix = s.defaultImagePrefix
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
		// TODO: what if prefix is empty? is default prefix already there?
		if prefixEntry, ok := imageEntry.Entry[parsedImage.Prefix()]; ok {
			// TODO: what if tag is empty?
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
		// TODO: what if prefix is empty?
		if prefixEntry, ok := imageEntry.Entry[parsedImage.Prefix()]; ok {
			tags := slices.Collect(maps.Keys(prefixEntry))
			best, err := imageutil.FindBestSemverMatch(constraint, parsedImage.BaseName, tags)
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
func (s *FunctionConfigStore) GetProcessor(imageName string) (fnsdk.ResourceListProcessor, bool) {
	config, ok := s.Get(imageName)
	if !ok {
		return nil, false
	}

	return getProcessorForConfig(&config)
}

func (s *FunctionConfigStore) GetProcessorByConstraint(imageName, constraint string) (fnsdk.ResourceListProcessor, bool) {
	config, ok := s.GetByConstraint(imageName, constraint)
	if !ok {
		return nil, false
	}

	return getProcessorForConfig(&config)
}

func getProcessorForConfig(config *configapi.FunctionConfigSpec) (fnsdk.ResourceListProcessor, bool) {
	if config.GoExecutor == nil {
		return nil, false
	}

	if config.GoExecutor.ID != nil {
		proc, ok := processorMapping[*config.GoExecutor.ID]
		return proc, ok
	}

	proc, ok := processorMapping[config.Image]
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

type ReconcilerFor string

const (
	ReconcilerForFunctionRunner ReconcilerFor = "function-runner"
	ReconcilerForServer         ReconcilerFor = "server"
	ReconcilerForController     ReconcilerFor = "controller"
)

type FunctionConfigReconciler struct {
	Client              client.Client
	FunctionConfigStore *FunctionConfigStore
	// For indicates which component the reconciler is collecting the configs for
	// TODO: remove after merging of function-runner into server
	For ReconcilerFor
}

func (r *FunctionConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, finalErr error) {
	klog.Infof("FunctionConfig %q changed", req.NamespacedName)
	obj := &configapi.FunctionConfig{}
	err := r.Client.Get(ctx, req.NamespacedName, obj)
	if apierrors.IsNotFound(err) {
		r.FunctionConfigStore.DeleteByObjName(req.NamespacedName)
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if obj.DeletionTimestamp != nil {
		if err := r.removeFinalizer(ctx, obj); err != nil {
			return ctrl.Result{}, err
		}

		r.FunctionConfigStore.Delete(obj.Spec.Image)
		return ctrl.Result{}, nil
	}

	if err := r.addFinalizer(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		patch := client.MergeFrom(obj.DeepCopy())

		if finalErr != nil {
			obj.Status.Error = finalErr.Error()
		} else {
			obj.Status.Error = ""
			switch r.For {
			case ReconcilerForFunctionRunner:
				obj.Status.FunctionRunnerObservedGeneration = obj.Generation
			case ReconcilerForServer:
				obj.Status.ApiServerObservedGeneration = obj.Generation
			case ReconcilerForController:
				obj.Status.ControllerObservedGeneration = obj.Generation
			}
		}

		if err := r.Client.Status().Patch(ctx, obj, patch); err != nil {
			klog.Errorf("Failed to update status of FunctionConfig %q: %v", obj.Name, err)
			if finalErr == nil {
				finalErr = err
			}
		}
	}()

	if err := r.FunctionConfigStore.Store(obj); err != nil {
		klog.Errorf("Failed to store FunctionConfig %q: %v", obj.Name, err)
		// TODO: we shouldn't have a requeue loop here if we can't insert into the cache, but if the user deletes the
		// conflicting config, then this one won't be applied until a new event
		return ctrl.Result{}, IgnoreConflict(err)
	}

	return ctrl.Result{}, nil
}

func (r *FunctionConfigReconciler) removeFinalizer(ctx context.Context, obj *configapi.FunctionConfig) error {
	patch := client.MergeFrom(obj.DeepCopy())

	switch r.For {
	case ReconcilerForFunctionRunner:
		controllerutil.RemoveFinalizer(obj, FunctionRunnerFinalizer)
	case ReconcilerForServer:
		controllerutil.RemoveFinalizer(obj, ServerFinalizer)
	case ReconcilerForController:
		controllerutil.RemoveFinalizer(obj, ControllerFinalizer)
	}

	if err := r.Client.Patch(ctx, obj, patch); err != nil {
		klog.Errorf("Failed to remove finalizer from FunctionConfig %q: %v", obj.Name, err)
		return err
	}

	return nil
}

func (r *FunctionConfigReconciler) addFinalizer(ctx context.Context, obj *configapi.FunctionConfig) error {
	patch := client.MergeFrom(obj.DeepCopy())

	updated := false
	switch r.For {
	case ReconcilerForFunctionRunner:
		updated = controllerutil.AddFinalizer(obj, FunctionRunnerFinalizer)
	case ReconcilerForServer:
		updated = controllerutil.AddFinalizer(obj, ServerFinalizer)
	case ReconcilerForController:
		updated = controllerutil.AddFinalizer(obj, ControllerFinalizer)
	}

	if updated {
		if err := r.Client.Patch(ctx, obj, patch); err != nil {
			klog.Errorf("Failed to add finalizer to FunctionConfig %q: %v", obj.Name, err)
			return err
		}
	}

	return nil
}

func IgnoreConflict(err error) error {
	if apierrors.IsConflict(err) {
		return nil
	}
	return err
}
