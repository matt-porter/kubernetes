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
	"time"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	"k8s.io/kubernetes/pkg/types"
)

type worker struct {
	// Channel of container IDs to probe.
	// Exit the probe loop when the channel is closed.
	targets chan kubecontainer.Container // FIXME better name

	// The pod containing this probe (read-only)
	pod *api.Pod

	// The container to probe (read-only)
	container *api.Container

	// Describe probe configuration (read-only)
	spec *api.Probe

	// The type of this worker.
	probeType probeType
}

// Probe type enum
type probeType bool

const (
	readiness probeType = true
	liveness  probeType = false
)

// Creates and starts a new probe worker.
func (m *manager) newWorker(
	pod *api.Pod,
	container *api.Container,
	probeType probeType) *worker {

	w := &worker{
		pod:       pod,
		container: container,
		probeType: probeType,
		targets:   make(chan kubecontainer.Container),
	}

	if probeType == readiness {
		w.spec = container.ReadinessProbe
	} else {
		w.spec = container.LivenessProbe
	}

	// Start the worker thread.
	go run(m, w)

	// If the container is already running, tell the worker.
	if cId, ok := m.containerCache.get(pod.UID, container.Name); ok {
		// Don't block: If another container is already incoming,
		// it's fresher than the cache.
		select {
		case w.targets <- cId:
		default:
		}
	}

	return w
}

// run periodically probes the container.
func run(m *manager, w *worker) {
	container, ok := <-w.targets
	probeTicker := time.Tick(m.defaultProbePeriod)

	// Handle new target container.
newContainer:
	if !ok {
		// No more targets, shutdown the worker.
		if w.probeType == readiness {
			m.readinessCache.remove(container.ID)
		} else {
			m.livenessCache.remove(container.ID)
		}
		return
	}

	remainingDelay := time.Now().Unix() - (container.Created + w.spec.InitialDelaySeconds)
	if remainingDelay > 0 {
		// FIXME - is this the right implementation? Might the state need to be recorded before probing even starts?
		if w.probeType == readiness {
			// Readiness probes default to Failure during initial delay.
			m.readinessCache.set(container.ID, Failure)
		}

		select {
		case container, ok = <-w.targets:
			// Container restarted during initial delay.
			goto newContainer
		case <-time.After(time.Duration(remainingDelay) * time.Second):
			// Carry on.
		}
	}

	for {
		select {
		case container, ok = <-w.targets:
			// Restart the probe loop with the new target.
			goto newContainer

		case <-probeTicker:
			doProbe(m, w, container.ID)
		}
	}
}

// doProbe probes the container once and records the result.
func doProbe(m *manager, w *worker, containerID types.UID) {
	status, ok := m.podStatusCache.GetPodStatus(w.pod.UID)
	if !ok {
		glog.Errorf("Status not set for pod %v (UID %v)",
			kubecontainer.GetPodFullName(w.pod), w.pod.UID)
		return
	}

	if w.probeType == readiness {
		// TODO: Move error handling out of prober.
		result, _ := m.prober.probeReadiness(w.pod, status, *w.container, string(containerID))
		m.readinessCache.set(containerID, result)
	} else { // probeType == liveness
		result, err := m.prober.probeLiveness(w.pod, status, *w.container, string(containerID))
		// If the liveness probe failed, skip reporting (state unchanged).
		if err == nil {
			prevResult, ok := m.livenessCache.get(containerID)
			m.livenessCache.set(containerID, result)
			if result != prevResult && ok {
				// Trigger a (non-blocking) sync if the probe result changed.
				select {
				case m.syncChan <- struct{}{}:
				default:
				}
			}
		}
	}
}
