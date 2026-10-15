// Copyright 2026 Microsoft Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configoverride

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const settingsFixture = `environments:
- name: ci01
  defaults:
    region: centralus
    regionShortOverride: "j${BUILD_ID: -7}"
- name: cspr
  defaults:
    region: westus3
- name: pers
  defaults:
    regionShortSuffix: "${USER:0:4}"
`

// The template action mirrors config.yaml, where unquoted actions parse as mapping keys.
const serviceConfigFixture = `defaults:
  azureRegionAvailabilityZoneCount: {{ .ev2.availabilityZoneLiveCount }}
  svc:
    aks:
      tags: "clusterType=svc-cluster,persist=true"
  mgmt:
    aks:
      tags: "clusterType=mgmt-cluster,JobID.ARO-HCP-CI.redhat.com=stale,persist=true"
clouds:
  dev:
    defaults:
      svc:
        aks:
          tags: "clusterType=svc-cluster,cloud=dev"
    environments:
      ci01:
        defaults:
          mgmt:
            aks:
              tags: null
`

func TestRunCostTag(t *testing.T) {
	settings := fixture(t, "settings.yaml", settingsFixture)
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	for _, tc := range []struct {
		name, settings, environment, buildID string
		want                                 string
		tagged                               bool
		wantError                            string
	}{
		{name: "per-run environment", settings: settings, environment: "ci01", buildID: "2110699886142820352", want: "2110699886142820352", tagged: true},
		{name: "long-lived environment", settings: settings, environment: "cspr", buildID: "2110699886142820352"},
		{name: "user-derived names", settings: settings, environment: "pers", buildID: "2110699886142820352"},
		{name: "unknown environment", settings: settings, environment: "test-env", buildID: "2110699886142820352"},
		{name: "outside Prow reads nothing", settings: missing, environment: "ci01"},
		{name: "blank job ID reads nothing", settings: missing, environment: "ci01", buildID: " \t"},
		{name: "invalid job ID", settings: settings, environment: "ci01", buildID: "12 34", wantError: "not a valid cost attribution tag value"},
		{name: "invalid job ID in long-lived environment", settings: settings, environment: "cspr", buildID: "12 34"},
		{name: "missing settings", settings: missing, environment: "ci01", buildID: "123", wantError: "read dev settings"},
		{name: "settings required", environment: "ci01", buildID: "123", wantError: "dev settings file is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, tagged, err := RunCostTag(tc.settings, tc.environment, tc.buildID)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("RunCostTag() error = %v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil || value != tc.want || tagged != tc.tagged {
				t.Fatalf("RunCostTag() = %q, %v, %v; want %q, %v", value, tagged, err, tc.want, tc.tagged)
			}
		})
	}
}

func TestRunAKSTags(t *testing.T) {
	config := fixture(t, "config.yaml", serviceConfigFixture)
	got, err := runAKSTags(config, "ci01", "123")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		// The dev cloud defaults take precedence over the global defaults.
		"svc": "clusterType=svc-cluster,cloud=dev,jobID.aro-hcp-ci.redhat.com=123",
		// A null environment value falls back, and a job ID in any key casing is replaced.
		"mgmt": "clusterType=mgmt-cluster,persist=true,jobID.aro-hcp-ci.redhat.com=123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runAKSTags() = %#v, want %#v", got, want)
	}

	if _, err := runAKSTags(fixture(t, "list.yaml", "defaults:\n  svc:\n    aks:\n      tags: [a, b]\n"), "ci01", "123"); err == nil || !strings.Contains(err.Error(), "comma-separated string") {
		t.Fatalf("non-string tags error = %v", err)
	}
	if _, err := runAKSTags("", "ci01", "123"); err == nil {
		t.Fatal("missing service configuration accepted")
	}
}

func TestWithRunCostTag(t *testing.T) {
	for _, tc := range []struct{ base, want string }{
		{"", "jobID.aro-hcp-ci.redhat.com=1"},
		{"a=b", "a=b,jobID.aro-hcp-ci.redhat.com=1"},
		{"a=b,,JOBID.ARO-HCP-CI.REDHAT.COM=0", "a=b,jobID.aro-hcp-ci.redhat.com=1"},
		{"jobID.aro-hcp-ci.redhat.com.extra=keep", "jobID.aro-hcp-ci.redhat.com.extra=keep,jobID.aro-hcp-ci.redhat.com=1"},
	} {
		if got := withRunCostTag(tc.base, "1"); got != tc.want {
			t.Fatalf("withRunCostTag(%q) = %q, want %q", tc.base, got, tc.want)
		}
	}
}

func TestGenerateRunCostTags(t *testing.T) {
	paths := Paths{DevSettings: fixture(t, "settings.yaml", settingsFixture), ServiceConfig: fixture(t, "config.yaml", serviceConfigFixture)}

	_, document := generated(t, Inputs{DeployEnvironment: "ci01", BuildID: "123"}, paths)
	want := map[string]interface{}{
		"svc": map[string]interface{}{"aks": map[string]interface{}{"tags": "clusterType=svc-cluster,cloud=dev,jobID.aro-hcp-ci.redhat.com=123"}},
		"mgmt": map[string]interface{}{"aks": map[string]interface{}{
			"tags":          "clusterType=mgmt-cluster,persist=true,jobID.aro-hcp-ci.redhat.com=123",
			"userAgentPool": map[string]interface{}{"minCount": 1},
		}},
	}
	if got := defaultsAt(t, document, "ci01"); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %#v, want %#v", got, want)
	}

	_, document = generated(t, Inputs{DeployEnvironment: "cspr", BuildID: "123", MSIContainers: "lease"}, paths)
	if len(document) != 0 {
		t.Fatalf("long-lived environment got overrides: %#v", document)
	}

	if _, err := Generate(Inputs{DeployEnvironment: "ci01", BuildID: "12 34"}, paths); err == nil {
		t.Fatal("invalid job ID accepted")
	}
}

// TestRepositoryRunCostTags guards the per-run classification and AKS tag paths
// against changes to the checked-in settings and service configuration.
func TestRepositoryRunCostTags(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	settings := filepath.Join(root, DefaultDevSettingsFile)
	config := filepath.Join(root, DefaultServiceConfigFile)
	for _, environment := range []string{"ci00", "ci01"} {
		value, tagged, err := RunCostTag(settings, environment, "2110699886142820352")
		if err != nil || !tagged || value != "2110699886142820352" {
			t.Fatalf("%s: RunCostTag() = %q, %v, %v; want a run tag", environment, value, tagged, err)
		}
		tags, err := runAKSTags(config, environment, value)
		if err != nil {
			t.Fatal(err)
		}
		for kind, clusterType := range map[string]string{"svc": "clusterType=svc-cluster", "mgmt": "clusterType=mgmt-cluster"} {
			if !strings.Contains(tags[kind], clusterType) || !strings.HasSuffix(tags[kind], ","+RunCostTagKey+"=2110699886142820352") {
				t.Fatalf("%s %s tags = %q", environment, kind, tags[kind])
			}
		}
	}
	for _, environment := range []string{"cspr", "dev", "pers", "perf", "dev-ci"} {
		if _, tagged, err := RunCostTag(settings, environment, "2110699886142820352"); err != nil || tagged {
			t.Fatalf("%s: RunCostTag() tagged=%v err=%v; long-lived environments must not be tagged", environment, tagged, err)
		}
	}
}
