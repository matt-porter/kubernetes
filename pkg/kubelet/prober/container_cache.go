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

	"k8s.io/kubernetes/pkg/api"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	"k8s.io/kubernetes/pkg/types"
)

// FIXME - double check documentation

// containerCache maintains the probe container of containers over time.
// It is thread-safe, no locks are necessary for the caller.
type containerCache struct {
	// guards tables
	sync.RWMutex

	// Lookup table from API object name to running container.
	containers map[containerPath]kubecontainer.Container
}

// newContainerCache creates and returns a containerCache with empty contents.
func newContainerCache() *containerCache {
	return &containerCache{
		containers: make(map[containerPath]kubecontainer.Container),
	}
}

// lookupContainerID returns the container ID associated with the pod & container pair,
// and whether it exists.
func (cc *containerCache) get(podUID types.UID, containerName string) (kubecontainer.Container, bool) {
	cc.RLock()
	defer cc.RUnlock()
	c, ok := cc.containers[containerPath{podUID, containerName}]
	return c, ok
}

// removePod removes containers in the pod from the cache
func (cc *containerCache) removePod(p *api.Pod) {
	cc.Lock()
	defer cc.Unlock()

	path := containerPath{podUID: p.UID}
	for _, c := range p.Spec.Containers {
		path.containerName = c.Name
		delete(cc.containers, path)
	}
}

// handleContainerUpdate updates the cache with new container IDs.
func (cc *containerCache) insertContainer(c *kubecontainer.Container) {
	cc.Lock()
	defer cc.Unlock()
	cc.containers[containerPath{c.PodUID, c.Name}] = *c
}
