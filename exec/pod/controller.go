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
	"fmt"
	"strings"

	"github.com/chaosblade-io/chaosblade-exec-cri/exec/container"
	"github.com/chaosblade-io/chaosblade-spec-go/spec"
	"github.com/chaosblade-io/chaosblade-spec-go/util"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"

	"github.com/chaosblade-io/chaosblade-operator/channel"
	"github.com/chaosblade-io/chaosblade-operator/exec/model"
	"github.com/chaosblade-io/chaosblade-operator/pkg/apis/chaosblade/v1alpha1"
)

type ExpController struct {
	model.BaseExperimentController
}

func NewExpController(client *channel.Client) model.ExperimentController {
	return &ExpController{
		model.BaseExperimentController{
			Client:            client,
			ResourceModelSpec: NewResourceModelSpec(client),
		},
	}
}

func (*ExpController) Name() string {
	return "pod"
}

// Create pod resource experiments
func (e *ExpController) Create(ctx context.Context, expSpec v1alpha1.ExperimentSpec) *spec.Response {
	expModel := model.ExtractExpModelFromExperimentSpec(expSpec)
	experimentId := model.GetExperimentIdFromContext(ctx)
	logrusField := logrus.WithField("experiment", experimentId)

	// Get action spec to check if it implements ActionPreProcessor
	actionSpec := e.ResourceModelSpec.GetExpActionModelSpec(expModel.Target, expModel.ActionName)
	if actionSpec != nil {
		if preProcessor, ok := actionSpec.(model.ActionPreProcessor); ok {
			newCtx, resp := preProcessor.PreCreate(ctx, expModel, e.Client)
			if resp != nil {
				return resp
			}
			ctx = newCtx
			logrusField.Infof("creating %s experiment with pre-processing", expModel.ActionName)
			return e.Exec(ctx, expModel)
		}
	}

	var checkpoint model.NetworkTargetCheckpoint
	if model.UsesProtocolList(expModel) {
		var ok bool
		checkpoint, ok = model.NetworkCheckpoint(ctx)
		if !ok || checkpoint.Save == nil {
			return spec.ReturnFail(spec.UnexpectedStatus, "missing durable network target checkpoint")
		}
		if checkpoint.Previous.State == model.NetworkTargetsReserved {
			return spec.ReturnSuccess(v1alpha1.CreateFailExperimentStatus("create result unknown after interrupted dispatch; recover the reserved targets", checkpoint.Previous.ResStatuses))
		}
	}
	// Default flow: find matched pods and execute
	pods, resp := e.GetMatchedPodResources(ctx, *expModel)
	if !resp.Success {
		logrusField.Errorf("uid: %s, get matched pod experiment failed, %v", experimentId, resp.Err)
		resp.Result = v1alpha1.CreateFailExperimentStatus(resp.Err, []v1alpha1.ResourceStatus{})
		return resp
	}
	logrusField.Infof("creating pod experiment, pod count is %d", len(pods))
	containerName := strings.TrimSpace(expModel.ActionFlags[model.ContainerNamesFlag.Name])
	containerObjectMetaList := getContainerMatchedList(experimentId, pods, containerName)
	logrusField.Infof("pod count: %d, matched container count: %d", len(pods), len(containerObjectMetaList))
	if len(containerObjectMetaList) == 0 {
		if containerName != "" {
			logrusField.WithField("container", containerName).Warn("no available target containers, skip injection")
			return spec.ReturnSuccess(v1alpha1.CreateSuccessExperimentStatus([]v1alpha1.ResourceStatus{}))
		}
		logrusField.Errorf("uid: %s, get container from context failed", experimentId)
		return spec.ResponseFailWithResult(spec.ContainerInContextNotFound,
			v1alpha1.CreateFailExperimentStatus(spec.ContainerInContextNotFound.Msg, []v1alpha1.ResourceStatus{}))
	}
	var reserved []v1alpha1.ResourceStatus
	if model.UsesProtocolList(expModel) {
		for i := range containerObjectMetaList {
			obj := &containerObjectMetaList[i]
			uid, err := util.GenerateUid()
			if err != nil {
				return spec.ReturnFail(spec.UnexpectedStatus, err.Error())
			}
			obj.Id = uid
			reserved = append(reserved, v1alpha1.ResourceStatus{Kind: expSpec.Scope, Id: uid, Identifier: obj.GetIdentifier()})
		}
		pending := v1alpha1.ExperimentStatus{Scope: expSpec.Scope, Target: expSpec.Target, Action: expSpec.Action, State: model.NetworkTargetsReserved, ResStatuses: reserved}
		if err := checkpoint.Save(pending); err != nil {
			return spec.ReturnFail(spec.UnexpectedStatus, "persist child UIDs: "+err.Error())
		}
	}
	ctx = model.SetContainerObjectMetaListToContext(ctx, containerObjectMetaList)
	return keepNetworkTargets(e.Exec(ctx, expModel), reserved)
}

func (e *ExpController) Destroy(ctx context.Context, expSpec v1alpha1.ExperimentSpec, oldExpStatus v1alpha1.ExperimentStatus) *spec.Response {
	expModel := model.ExtractExpModelFromExperimentSpec(expSpec)
	experimentId := model.GetExperimentIdFromContext(ctx)
	logrus.WithField("experiment", experimentId).Infoln("start to destroy")

	// Check if action implements ActionPreProcessor - use the same path as Create
	actionSpec := e.ResourceModelSpec.GetExpActionModelSpec(expModel.Target, expModel.ActionName)
	if actionSpec != nil {
		if preProcessor, ok := actionSpec.(model.ActionPreProcessor); ok {
			newCtx, resp := preProcessor.PreDestroy(ctx, expModel, e.Client, oldExpStatus)
			if resp != nil {
				return resp
			}
			ctx = newCtx
			return e.Exec(ctx, expModel)
		}
	}

	// Default flow: find matched containers and destroy
	statuses := oldExpStatus.ResStatuses
	if len(statuses) == 0 {
		return spec.ReturnSuccess(v1alpha1.CreateSuccessExperimentStatus([]v1alpha1.ResourceStatus{}))
	}
	containerObjectMetaList := model.ContainerMatchedList{}
	for _, status := range statuses {
		// On a destroy retry, a failed resource still owns rules under its child UID.
		if model.UsesProtocolList(expModel) && status.Id == "" && status.Identifier != "" {
			return spec.ReturnSuccess(v1alpha1.CreateFailExperimentStatus("child UID is unknown; cannot confirm recovery", statuses))
		}
		if !status.Success && !(model.UsesProtocolList(expModel) && status.Id != "") {
			continue
		}
		containerObjectMeta := model.ParseIdentifier(status.Identifier)
		containerObjectMeta.Id = status.Id
		containerObjectMetaList = append(containerObjectMetaList, containerObjectMeta)
	}
	if len(containerObjectMetaList) == 0 {
		return spec.ReturnSuccess(v1alpha1.CreateSuccessExperimentStatus([]v1alpha1.ResourceStatus{}))
	}
	ctx = model.SetContainerObjectMetaListToContext(ctx, containerObjectMetaList)
	response := e.Exec(ctx, expModel)
	if model.UsesProtocolList(expModel) {
		return keepNetworkTargets(response, statuses)
	}
	return response
}

// Errors before per-target execution may have no structured result at all.
func keepNetworkTargets(response *spec.Response, targets []v1alpha1.ResourceStatus) *spec.Response {
	if len(targets) == 0 {
		return response
	}
	status, ok := response.Result.(v1alpha1.ExperimentStatus)
	if !ok && !response.Success {
		status = v1alpha1.CreateFailExperimentStatus(response.Err, nil)
	}
	if !status.Success && len(status.ResStatuses) == 0 {
		status.ResStatuses = targets
		response.Result = status
	}
	return response
}

// getContainerMatchedList transports selected pods
func getContainerMatchedList(experimentId string, pods []v1.Pod, targetContainerName string) model.ContainerMatchedList {
	containerObjectMetaList := model.ContainerMatchedList{}
	for _, p := range pods {
		var containerId, runtime string
		var err error
		containerName := targetContainerName
		if targetContainerName == "" {
			containerId, containerName, runtime, err = model.GetOneAvailableContainerIdFromPod(p)
		} else {
			containerId, runtime, err = getNamedContainerFromPod(p, targetContainerName)
		}
		if err != nil {
			entry := logrus.WithFields(logrus.Fields{
				"experiment": experimentId,
				"namespace":  p.Namespace,
				"pod":        p.Name,
				"container":  containerName,
			})
			if targetContainerName == "" {
				entry.Errorf("get an available container failed, %v", err)
			} else {
				entry.Warnf("skip target container: %v", err)
			}
			continue
		}
		if runtime == container.DockerRuntime && len(containerId) > 12 {
			containerId = containerId[:12]
		}
		containerObjectMetaList = append(containerObjectMetaList, model.ContainerObjectMeta{
			ContainerId:      containerId,
			ContainerName:    containerName,
			ContainerRuntime: runtime,
			PodName:          p.Name,
			NodeName:         p.Spec.NodeName,
			Namespace:        p.Namespace,
		})
	}
	return containerObjectMetaList
}

func getNamedContainerFromPod(pod v1.Pod, name string) (containerID, runtime string, err error) {
	for _, target := range pod.Spec.Containers {
		if target.Name != name {
			continue
		}
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name != name {
				continue
			}
			if status.State.Running == nil {
				return "", "", fmt.Errorf("container is not running")
			}
			if status.ContainerID == "" {
				return "", "", fmt.Errorf("container ID is empty")
			}
			runtime, containerID = model.TruncateContainerObjectMetaUid(status.ContainerID)
			if containerID == "" || runtime == "" {
				return "", "", fmt.Errorf("container ID is invalid")
			}
			return containerID, runtime, nil
		}
		return "", "", fmt.Errorf("container status is unavailable")
	}
	return "", "", fmt.Errorf("container not found")
}
