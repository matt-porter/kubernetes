/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package prober

import (
	"sync"

	"k8s.io/kubernetes/pkg/types"
)

// FIXME - double check documentation

// resultsCache maintains the probe results of containers over time.
// It is thread-safe, no locks are necessary for the caller.
type resultsCache struct {
	// guards states
	sync.RWMutex
	// holds current results.
	results map[types.UID]Result
}

// newResultsCache creates and returns a resultsCache with empty contents.
func newResultsCache() *resultsCache {
	return &resultsCache{results: make(map[types.UID]Result)}
}

// getResult returns the  value for the container with the given ID.
func (r *resultsCache) get(id types.UID) (Result, bool) {
	r.RLock()
	defer r.RUnlock()
	res, ok := r.results[id]
	return res, ok
}

// setResult sets the Result for the container with the given ID.
func (r *resultsCache) set(id types.UID, value Result) {
	r.Lock()
	defer r.Unlock()
	r.results[id] = value
}

// removeResult clears the Result for the container with the given ID.
func (r *resultsCache) remove(id types.UID) {
	r.Lock()
	defer r.Unlock()
	delete(r.results, id)
}
