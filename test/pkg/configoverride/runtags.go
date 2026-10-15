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
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// RunCostTagKey attributes Azure spend to the Prow job that created it. Cost
// Management tag inheritance on the E2E subscriptions applies resource group tags
// to the usage records of everything inside, so one filter on this key shows a
// job's spend across subscriptions.
const RunCostTagKey = "jobID.aro-hcp-ci.redhat.com"

// Source-relative defaults for the files RunCostTag and the AKS tag override read.
const (
	DefaultDevSettingsFile   = "tooling/templatize/settings.yaml"
	DefaultServiceConfigFile = "config/config.yaml"
)

var runCostTagValue = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// RunCostTag returns the job's cost attribution tag value when deployEnvironment
// is a per-run environment: one whose resource names derive from BUILD_ID in the
// templatize dev settings. Long-lived environments share their resources across
// jobs, so they get no tag rather than the ID of whichever job deployed them last.
func RunCostTag(devSettingsPath, deployEnvironment, buildID string) (string, bool, error) {
	if strings.TrimSpace(buildID) == "" {
		return "", false, nil
	}
	if devSettingsPath == "" {
		return "", false, errors.New("dev settings file is required to decide run cost attribution")
	}
	data, err := os.ReadFile(devSettingsPath)
	if err != nil {
		return "", false, fmt.Errorf("read dev settings %q: %w", devSettingsPath, err)
	}
	var settings struct {
		Environments []struct {
			Name     string `yaml:"name"`
			Defaults struct {
				RegionShortOverride string `yaml:"regionShortOverride"`
			} `yaml:"defaults"`
		} `yaml:"environments"`
	}
	if err := decodeYAML(data, &settings); err != nil {
		return "", false, fmt.Errorf("parse dev settings %q: %w", devSettingsPath, err)
	}
	perRun := false
	for _, environment := range settings.Environments {
		if environment.Name == deployEnvironment {
			perRun = strings.Contains(environment.Defaults.RegionShortOverride, "${BUILD_ID")
			break
		}
	}
	if !perRun {
		return "", false, nil
	}
	if !runCostTagValue.MatchString(buildID) {
		return "", false, fmt.Errorf("BUILD_ID %q is not a valid cost attribution tag value", buildID)
	}
	return buildID, true, nil
}

// runAKSTags returns the service and management AKS cluster tags with the run cost
// tag added. AKS copies cluster tags onto each node resource group. The override
// replaces the configured comma-separated tags, so it starts from the effective
// configured value.
func runAKSTags(serviceConfigPath, deployEnvironment, value string) (map[string]string, error) {
	if serviceConfigPath == "" {
		return nil, errors.New("service configuration file is required to add run cost tags")
	}
	data, err := os.ReadFile(serviceConfigPath)
	if err != nil {
		return nil, fmt.Errorf("read service configuration %q: %w", serviceConfigPath, err)
	}
	// The service configuration is a template: unquoted template actions such as
	// {{ .ev2.value }} parse as mapping keys, so walk nodes instead of decoding to maps.
	var document yaml.Node
	if err := decodeYAML(data, &document); err != nil {
		return nil, fmt.Errorf("parse service configuration %q: %w", serviceConfigPath, err)
	}
	tags := map[string]string{}
	for _, kind := range []string{"svc", "mgmt"} {
		base, err := configuredAKSTags(&document, deployEnvironment, kind)
		if err != nil {
			return nil, fmt.Errorf("service configuration %q: %w", serviceConfigPath, err)
		}
		tags[kind] = withRunCostTag(base, value)
	}
	return tags, nil
}

// configuredAKSTags resolves the dev environment, dev cloud and global defaults in
// the order templatize applies them.
func configuredAKSTags(document *yaml.Node, deployEnvironment, kind string) (string, error) {
	for _, path := range [][]string{
		{"clouds", "dev", "environments", deployEnvironment, "defaults", kind, "aks", "tags"},
		{"clouds", "dev", "defaults", kind, "aks", "tags"},
		{"defaults", kind, "aks", "tags"},
	} {
		node := lookup(document, path)
		if node == nil || node.Tag == "!!null" {
			continue
		}
		if node.Kind != yaml.ScalarNode {
			return "", fmt.Errorf("%s must be a comma-separated string", strings.Join(path, "."))
		}
		return node.Value, nil
	}
	return "", nil
}

// lookup returns the node at path in a document, or nil when any key is absent.
func lookup(document *yaml.Node, path []string) *yaml.Node {
	current := document
	if current.Kind == yaml.DocumentNode && len(current.Content) == 1 {
		current = current.Content[0]
	}
	for _, key := range path {
		if current.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for i := 0; i+1 < len(current.Content); i += 2 {
			if current.Content[i].Kind == yaml.ScalarNode && current.Content[i].Value == key {
				next = current.Content[i+1]
			}
		}
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

// withRunCostTag appends the run cost tag to comma-separated key=value tags,
// replacing an existing entry. Azure tag names are case-insensitive.
func withRunCostTag(tags, value string) string {
	var result []string
	for _, tag := range strings.Split(tags, ",") {
		key, _, _ := strings.Cut(tag, "=")
		if tag != "" && !strings.EqualFold(key, RunCostTagKey) {
			result = append(result, tag)
		}
	}
	return strings.Join(append(result, RunCostTagKey+"="+value), ",")
}
