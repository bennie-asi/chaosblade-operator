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
	"github.com/chaosblade-io/chaosblade-operator/pkg/apis/chaosblade/v1alpha1"
	"github.com/chaosblade-io/chaosblade-spec-go/spec"
	"testing"
)

func TestKeepReservedUIDWhenDispatchReturnsNoStatus(t *testing.T) {
	reserved := []v1alpha1.ResourceStatus{{Id: "child", Identifier: "ns/node/pod/container/id/containerd"}}
	for _, response := range []*spec.Response{spec.ReturnFail(spec.UnexpectedStatus, "stream closed"), spec.ReturnSuccess(v1alpha1.CreateFailExperimentStatus("lost response", nil))} {
		status := keepNetworkTargets(response, reserved).Result.(v1alpha1.ExperimentStatus)
		if status.Success || len(status.ResStatuses) != 1 || status.ResStatuses[0].Id != "child" || status.ResStatuses[0].Identifier != reserved[0].Identifier {
			t.Fatal(status)
		}
	}
}
