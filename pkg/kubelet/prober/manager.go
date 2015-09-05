/*
Copyright 2014 The Kubernetes Authors All rights reserved.

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
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/unversioned/record"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	"k8s.io/kubernetes/pkg/types"
)

type Manager interface {
	// FIXME - docs
	AddPod(pod *api.Pod)
	RemovePod(pod *api.Pod)

	// HandleNewContainer notifies the Manager of new containers
	// that are running. Must be thread-safe.
	HandleNewContainer(c kubecontainer.Container)

	// GetReadiness reports the state of the readiness probe for the given container.
	// Defaults to Success for un-probed containers.
	GetReadiness(id types.UID) Result
	// GetLiveness reports the state of the liveness probe for the given container.
	// Defaults to Success for un-probed containers.
	GetLiveness(id types.UID) Result
}

// Probe result type
type Result bool

const (
	Success Result = true
	Failure Result = false
)

// FIXME - DOC
type PodStatusCache interface {
	// Gets the cached pod status (thread safe)
	// Takes the api.Pod.UID, and returns the cached pod status and whether it was a cache hit.
	GetPodStatus(uid types.UID) (api.PodStatus, bool)
}

type manager struct {
	// Caches the results of liveness probes.
	livenessCache *resultsCache
	// Caches the results of readiness probes.
	readinessCache *resultsCache
	// Caches the running container IDs.
	containerCache *containerCache

	// Map of active workers for liveness and readiness
	livenessProbes  map[containerPath]*worker
	readinessProbes map[containerPath]*worker
	// Lock for accessing & mutating livenessProbes and readinessProbes
	workerLock sync.RWMutex

	// Triggers a sync in kubelet syncLoopIteration
	syncChan chan struct{}

	// FIXME - document these
	podStatusCache PodStatusCache
	prober         *prober

	// Default probe period, when none is specified.
	defaultProbePeriod time.Duration
}

func NewManager(
	syncChan chan struct{},
	defaultProbePeriod time.Duration,
	podStatusCache PodStatusCache,
	recorder record.EventRecorder,
	refManager *kubecontainer.RefManager,
	runner kubecontainer.ContainerCommandRunner) Manager {
	return &manager{
		syncChan:           syncChan,
		defaultProbePeriod: defaultProbePeriod,
		podStatusCache:     podStatusCache,
		prober:             newProber(runner, refManager, recorder),
		livenessCache:      newResultsCache(),
		readinessCache:     newResultsCache(),
		containerCache:     newContainerCache(),
		livenessProbes:     make(map[containerPath]*worker),
		readinessProbes:    make(map[containerPath]*worker),
	}
}

// Key uniquely identifying containers
type containerPath struct {
	podUID        types.UID
	containerName string
}

func (m *manager) AddPod(pod *api.Pod) {
	m.workerLock.Lock()
	defer m.workerLock.Unlock()

	key := containerPath{podUID: pod.UID}
	for _, c := range pod.Spec.Containers {
		key.containerName = c.Name
		if c.ReadinessProbe != nil {
			m.readinessProbes[key] = m.newWorker(pod, &c, readiness)
		}
		if c.LivenessProbe != nil {
			m.livenessProbes[key] = m.newWorker(pod, &c, liveness)
		}
	}
}

func (m *manager) RemovePod(pod *api.Pod) {
	m.containerCache.removePod(pod)

	m.workerLock.Lock()
	defer m.workerLock.Unlock()

	key := containerPath{podUID: pod.UID}
	for _, c := range pod.Spec.Containers {
		key.containerName = c.Name
		if worker, ok := m.readinessProbes[key]; ok {
			close(worker.targets)
			delete(m.readinessProbes, key)
		}
		if worker, ok := m.livenessProbes[key]; ok {
			close(worker.targets)
			delete(m.livenessProbes, key)
		}
	}
}

func (m *manager) HandleNewContainer(c kubecontainer.Container) {
	m.containerCache.insertContainer(&c)

	m.workerLock.RLock()
	defer m.workerLock.RUnlock()

	key := containerPath{c.PodUID, c.Name}
	if probe, ok := m.livenessProbes[key]; ok {
		probe.targets <- c
	}
	if probe, ok := m.readinessProbes[key]; ok {
		probe.targets <- c
	}
}

func (m *manager) GetReadiness(id types.UID) Result {
	if result, ok := m.readinessCache.get(id); ok {
		return result
	} else {
		return Success
	}
}

func (m *manager) GetLiveness(id types.UID) Result {
	if result, ok := m.livenessCache.get(id); ok {
		return result
	} else {
		return Success
	}
}
