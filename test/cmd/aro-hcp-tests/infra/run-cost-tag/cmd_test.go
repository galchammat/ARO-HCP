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

package runcosttag

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const settings = `environments:
- name: ci01
  defaults:
    regionShortOverride: "j${BUILD_ID: -7}"
- name: cspr
  defaults:
    region: westus3
`

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	command := NewCommand()
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), err
}

func TestRunCostTagCommand(t *testing.T) {
	source := t.TempDir()
	settingsFile := filepath.Join(source, "tooling", "templatize", "settings.yaml")
	if err := os.MkdirAll(filepath.Dir(settingsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsFile, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, environment, buildID, want, wantError string
		args                                        []string
	}{
		{name: "per-run environment", environment: "ci01", buildID: "2110699886142820352", want: "jobID.aro-hcp-ci.redhat.com=2110699886142820352\n"},
		{name: "deploy env flag", environment: "cspr", buildID: "2110699886142820352", args: []string{"--deploy-env", "ci01"}, want: "jobID.aro-hcp-ci.redhat.com=2110699886142820352\n"},
		{name: "long-lived environment", environment: "cspr", buildID: "2110699886142820352"},
		{name: "outside Prow", environment: "ci01"},
		{name: "invalid job ID", environment: "ci01", buildID: "12 34", wantError: "not a valid cost attribution tag value"},
		{name: "deployment environment required", buildID: "123", wantError: "--deploy-env or DEPLOY_ENV is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DEPLOY_ENV", tc.environment)
			t.Setenv("BUILD_ID", tc.buildID)
			stdout, err := execute(t, append([]string{"--source-dir", source}, tc.args...)...)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error = %v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil || stdout != tc.want {
				t.Fatalf("stdout = %q, err = %v; want %q", stdout, err, tc.want)
			}
		})
	}
}
