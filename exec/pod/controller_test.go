/*
 * Copyright 2025 The ChaosBlade Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package pod

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/chaosblade-io/chaosblade-operator/exec/model"
	"github.com/chaosblade-io/chaosblade-operator/pkg/apis/chaosblade/v1alpha1"
)

func containerTargetPod() v1.Pod {
	return v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "app-pod", Namespace: "default"},
		Spec: v1.PodSpec{
			NodeName:   "node-a",
			Containers: []v1.Container{{Name: "sidecar"}, {Name: "server"}},
		},
		Status: v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{
			{Name: "sidecar", ContainerID: "containerd://sidecar-id", State: v1.ContainerState{Running: &v1.ContainerStateRunning{}}},
			{Name: "server", ContainerID: "containerd://server-id", State: v1.ContainerState{Running: &v1.ContainerStateRunning{}}},
		}},
	}
}

func TestGetContainerMatchedList(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		change    func(*v1.Pod)
		wantName  string
		wantID    string
		wantCount int
	}{
		{name: "legacy selects first running", wantName: "sidecar", wantID: "sidecar-id", wantCount: 1},
		{name: "select second container by name", target: "server", wantName: "server", wantID: "server-id", wantCount: 1},
		{
			name: "status order differs from spec", target: "server", wantName: "server", wantID: "server-id", wantCount: 1,
			change: func(p *v1.Pod) {
				p.Status.ContainerStatuses[0], p.Status.ContainerStatuses[1] = p.Status.ContainerStatuses[1], p.Status.ContainerStatuses[0]
			},
		},
		{
			name: "unavailable sidecar does not affect target", target: "server", wantName: "server", wantID: "server-id", wantCount: 1,
			change: func(p *v1.Pod) {
				p.Status.ContainerStatuses[0].State = v1.ContainerState{Waiting: &v1.ContainerStateWaiting{}}
			},
		},
		{name: "missing name does not fall back", target: "missing"},
		{
			name: "waiting target does not fall back", target: "server",
			change: func(p *v1.Pod) {
				p.Status.ContainerStatuses[1].State = v1.ContainerState{Waiting: &v1.ContainerStateWaiting{}}
			},
		},
		{
			name: "terminated target does not fall back", target: "server",
			change: func(p *v1.Pod) {
				p.Status.ContainerStatuses[1].State = v1.ContainerState{Terminated: &v1.ContainerStateTerminated{}}
			},
		},
		{
			name: "missing status", target: "server",
			change: func(p *v1.Pod) { p.Status.ContainerStatuses = p.Status.ContainerStatuses[:1] },
		},
		{
			name: "missing runtime ID", target: "server",
			change: func(p *v1.Pod) { p.Status.ContainerStatuses[1].ContainerID = "" },
		},
		{
			name: "init container excluded", target: "init",
			change: func(p *v1.Pod) {
				p.Spec.InitContainers = []v1.Container{{Name: "init"}}
				p.Status.InitContainerStatuses = []v1.ContainerStatus{
					{Name: "init", ContainerID: "containerd://init-id", State: v1.ContainerState{Running: &v1.ContainerStateRunning{}}},
				}
			},
		},
		{
			name: "docker runtime", target: "server", wantName: "server", wantID: "0123456789ab", wantCount: 1,
			change: func(p *v1.Pod) { p.Status.ContainerStatuses[1].ContainerID = "docker://0123456789abcdef" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := containerTargetPod()
			if tt.change != nil {
				tt.change(&pod)
			}
			got := getContainerMatchedList("test-uid", []v1.Pod{pod}, tt.target)
			if len(got) != tt.wantCount {
				t.Fatalf("matched %d containers, want %d: %+v", len(got), tt.wantCount, got)
			}
			if len(got) > 0 && (got[0].ContainerName != tt.wantName || got[0].ContainerId != tt.wantID) {
				t.Fatalf("selected %+v, want %s/%s", got[0], tt.wantName, tt.wantID)
			}
		})
	}
}

func TestNamedContainerSelectionSkipsOnlyUnavailablePods(t *testing.T) {
	unavailable := containerTargetPod()
	unavailable.Name = "waiting-pod"
	unavailable.Status.ContainerStatuses[1].State = v1.ContainerState{Waiting: &v1.ContainerStateWaiting{}}
	got := getContainerMatchedList("test-uid", []v1.Pod{unavailable, containerTargetPod()}, "server")
	if len(got) != 1 || got[0].PodName != "app-pod" || got[0].ContainerName != "server" {
		t.Fatalf("unexpected matches: %+v", got)
	}
}

func TestPodCreateWithNoAvailableNamedContainer(t *testing.T) {
	for _, target := range []string{"cpu", "mem", "disk"} {
		t.Run(target, func(t *testing.T) {
			pod := containerTargetPod()
			client := newDnsTestClient(t, &pod)
			controller := NewExpController(client)
			action := "load"
			if target == "disk" {
				action = "fill"
			}
			experiment := v1alpha1.ExperimentSpec{
				Scope: "pod", Target: target, Action: action,
				Matchers: []v1alpha1.FlagSpec{
					{Name: "namespace", Value: []string{"default"}},
					{Name: "names", Value: []string{"app-pod"}},
					{Name: "container-names", Value: []string{"missing"}},
				},
			}
			response := controller.Create(context.Background(), experiment)
			if !response.Success {
				t.Fatalf("unavailable target should be skipped: %+v", response)
			}
			status, ok := response.Result.(v1alpha1.ExperimentStatus)
			if !ok || !status.Success || len(status.ResStatuses) != 0 {
				t.Fatalf("expected successful empty result, got %#v", response.Result)
			}
			if response := controller.Destroy(context.Background(), experiment, status); !response.Success {
				t.Fatalf("empty experiment recovery failed: %+v", response)
			}
		})
	}
}

func TestPodLegacyEmptyContainerSelectionStillFails(t *testing.T) {
	pod := containerTargetPod()
	pod.Status.ContainerStatuses = nil
	controller := NewExpController(newDnsTestClient(t, &pod))
	response := controller.Create(context.Background(), v1alpha1.ExperimentSpec{
		Scope: "pod", Target: "cpu", Action: "load",
		Matchers: []v1alpha1.FlagSpec{
			{Name: "namespace", Value: []string{"default"}},
			{Name: "names", Value: []string{"app-pod"}},
		},
	})
	if response.Success {
		t.Fatal("legacy empty selection must preserve its failure behavior")
	}
}

func TestPodContainerNameMatcherScope(t *testing.T) {
	resource := NewResourceModelSpec(nil)
	for target, command := range resource.ExpModels() {
		for _, action := range command.Actions() {
			want := target == "cpu" && action.Name() == "fullload" || target == "mem" && action.Name() == "load" ||
				target == "disk" && (action.Name() == "burn" || action.Name() == "fill")
			found := false
			for _, flag := range action.Matchers() {
				if flag.FlagName() == model.ContainerNamesFlag.Name {
					found = true
				}
			}
			for _, flag := range command.Flags() {
				if flag.FlagName() == model.ContainerNamesFlag.Name {
					found = true
				}
			}
			if found != want {
				t.Errorf("%s %s container-names matcher = %t, want %t", target, action.Name(), found, want)
			}
		}
	}
}
