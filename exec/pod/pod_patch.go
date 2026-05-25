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
	"encoding/json"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func patchPodImagesAndAnnotations(ctx context.Context, client crclient.Client, pod *v1.Pod, containers []v1.Container, annotations map[string]string) error {
	patchBytes, err := buildPodImagesAndAnnotationsPatch(containers, annotations)
	if err != nil || len(patchBytes) == 0 {
		return err
	}
	return client.Patch(ctx, pod, crclient.RawPatch(types.StrategicMergePatchType, patchBytes))
}

func buildPodImagesAndAnnotationsPatch(containers []v1.Container, annotations map[string]string) ([]byte, error) {
	if len(containers) == 0 && len(annotations) == 0 {
		return nil, nil
	}
	patchContainers := make([]v1.Container, 0, len(containers))
	for _, container := range containers {
		patchContainers = append(patchContainers, v1.Container{
			Name:  container.Name,
			Image: container.Image,
		})
	}
	patch := struct {
		Metadata metav1.ObjectMeta `json:"metadata,omitempty"`
		Spec     struct {
			Containers []v1.Container `json:"containers,omitempty"`
		} `json:"spec,omitempty"`
	}{
		Metadata: metav1.ObjectMeta{Annotations: annotations},
	}
	patch.Spec.Containers = patchContainers
	return json.Marshal(patch)
}
