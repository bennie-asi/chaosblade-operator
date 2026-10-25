package v1alpha1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestPublishedCRDsRequireOptionalObjectResult(t *testing.T) {
	for _, file := range []string{
		"deploy/crds/chaosblade.io_chaosblades_crd.yaml",
		"deploy/helm/chaosblade-operator/crds/crd.yaml",
		"deploy/helm/chaosblade-operator-arm64/crds/crd.yaml",
	} {
		t.Run(file, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("../../../..", file))
			if err != nil {
				t.Fatal(err)
			}
			var crd map[string]interface{}
			if err := yaml.Unmarshal(content, &crd); err != nil {
				t.Fatal(err)
			}
			versions := crd["spec"].(map[string]interface{})["versions"].([]interface{})
			for _, version := range versions {
				node := version.(map[string]interface{})
				for _, field := range []string{
					"schema", "openAPIV3Schema", "properties", "status", "properties", "expStatuses",
					"items", "properties", "resStatuses", "items",
				} {
					node = node[field].(map[string]interface{})
				}
				required, _ := node["required"].([]interface{})
				for _, field := range required {
					if field == "result" {
						t.Fatal("result must remain optional")
					}
				}
				detail := node["properties"].(map[string]interface{})["result"].(map[string]interface{})
				if detail["type"] != "object" || detail["x-kubernetes-preserve-unknown-fields"] != true {
					t.Fatalf("result must only allow a detail object: %v", detail)
				}
			}
		})
	}
}

func TestResourceStatusCanReadLegacyDestroyString(t *testing.T) {
	status := ResourceStatus{Result: json.RawMessage(`"command: cri disk fill, destroy time: 2026-10-25T06:07:56Z"`)}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ResourceStatus
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded.Result) != string(status.Result) {
		t.Fatalf("destroy result changed: %s", decoded.Result)
	}
}

func TestOptionalResultDoesNotChangeResourceOutcome(t *testing.T) {
	status := ResourceStatus{Id: "child-id", State: SuccessState, Success: true}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, present := fields["result"]; present {
		t.Fatalf("optional result was written: %s", encoded)
	}
	if fields["id"] != "child-id" || fields["state"] != SuccessState || fields["success"] != true {
		t.Fatalf("resource outcome changed: %s", encoded)
	}
}
