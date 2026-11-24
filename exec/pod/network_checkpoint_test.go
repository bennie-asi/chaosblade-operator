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
	"testing"

	"github.com/chaosblade-io/chaosblade-spec-go/spec"

	"github.com/chaosblade-io/chaosblade-operator/pkg/apis/chaosblade/v1alpha1"
)

func TestKeepReservedUIDWhenDispatchReturnsNoStatus(t *testing.T) {
	want := v1alpha1.ResourceStatus{Id: "child", Identifier: "ns/node/pod/container/id/containerd"}
	tests := []struct {
		name     string
		response *spec.Response
	}{
		{"stream closed", spec.ReturnFail(spec.UnexpectedStatus, "stream closed")},
		{"lost response", spec.ReturnSuccess(v1alpha1.CreateFailExperimentStatus("lost response", nil))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := keepNetworkTargets(tt.response, []v1alpha1.ResourceStatus{want}).Result
			status, ok := result.(v1alpha1.ExperimentStatus)
			if !ok {
				t.Fatalf("result has type %T, want ExperimentStatus", result)
			}
			if status.Success {
				t.Error("dispatch without a target status reported success")
			}
			if got := len(status.ResStatuses); got != 1 {
				t.Fatalf("got %d target statuses, want 1", got)
			}
			got := status.ResStatuses[0]
			if got.Id != want.Id || got.Identifier != want.Identifier {
				t.Errorf("got UID %q, target %q; want UID %q, target %q", got.Id, got.Identifier, want.Id, want.Identifier)
			}
		})
	}
}
