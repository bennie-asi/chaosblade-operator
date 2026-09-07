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

package model

import (
	"encoding/json"
	"testing"

	"github.com/chaosblade-io/chaosblade-operator/pkg/apis/chaosblade/v1alpha1"
	"github.com/chaosblade-io/chaosblade-spec-go/spec"
)

func TestDecodeCreateResultLegacyUID(t *testing.T) {
	uid, detail, err := decodeCreateResult("legacy-id")
	if err != nil || uid != "legacy-id" || detail != nil {
		t.Fatalf("uid=%q detail=%s err=%v", uid, detail, err)
	}
}

func TestDecodeCreateResultEnvelope(t *testing.T) {
	value := map[string]interface{}{
		"uid":    "pool-id",
		"detail": map[string]interface{}{"state": "ACTIVE", "actualHold": float64(4)},
	}
	uid, detail, err := decodeCreateResult(value)
	if err != nil || uid != "pool-id" {
		t.Fatalf("uid=%q err=%v", uid, err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(detail, &decoded); err != nil || decoded["state"] != "ACTIVE" {
		t.Fatalf("detail=%s err=%v", detail, err)
	}
}

func TestDecodeCreateResultRejectsMissingUID(t *testing.T) {
	_, _, err := decodeCreateResult(map[string]interface{}{
		"detail": map[string]interface{}{"state": "ACTIVE"},
	})
	if err == nil {
		t.Fatal("expected envelope without uid to fail")
	}
}

func TestNormalizeResultPreservesJSONObject(t *testing.T) {
	raw := normalizeResult(`{"state":"RELEASED","closedCount":4}`)
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["state"] != "RELEASED" {
		t.Fatalf("unexpected result: %s", raw)
	}
}

func TestCompensationIsDatasourceSpecific(t *testing.T) {
	statuses := []v1alpha1.ResourceStatus{{Id: "child", Success: true}}
	model := &spec.ExpModel{Target: "jvm", ActionName: "full-gc"}
	actual := compensateDatasourceCreate(model, statuses, nil, nil)
	if !actual[0].Success || actual[0].Id != "child" {
		t.Fatalf("non-datasource status changed: %+v", actual[0])
	}
}
