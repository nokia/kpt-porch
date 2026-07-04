// Copyright 2022, 2026 The kpt Authors
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

package oci

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
)

type Registries interface {
	FindRegistry(ctx context.Context, key string) (*Registry, error)
}

func NewDynamicRegistries(baseDir string) *DynamicRegistries {
	return &DynamicRegistries{
		baseDir: baseDir,
		repos:   make(map[string]*dynamicRegistry),
	}
}

type DynamicRegistries struct {
	mutex   sync.Mutex
	repos   map[string]*dynamicRegistry
	baseDir string
}

type dynamicRegistry struct {
	mutex    sync.Mutex
	registry *Registry
	name     string
	dir      string
}

func isRegistryIDAllowed(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			// OK
		} else if r >= '0' && r <= '9' {
			// OK
		} else {
			switch r {
			case '-':
				// OK
			case '/':
				// Allowed (!)
			default:
				return false
			}
		}
	}
	return true
}

func (r *DynamicRegistries) FindRegistry(ctx context.Context, id string) (*Registry, error) {
	dir := filepath.Join(r.baseDir, id)
	if !isRegistryIDAllowed(id) {
		return nil, fmt.Errorf("invalid name %q", id)
	}

	r.mutex.Lock()
	repo := r.repos[id]
	if repo == nil {
		repo = &dynamicRegistry{
			name: id,
			dir:  dir,
		}
		r.repos[id] = repo
	}
	r.mutex.Unlock()

	return repo.open()
}

func (r *dynamicRegistry) open() (*Registry, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.registry == nil {
		baseDir := r.dir

		registry, err := NewRegistry(r.name, baseDir)
		if err != nil {
			return nil, err
		}
		r.registry = registry
	}

	return r.registry, nil
}
