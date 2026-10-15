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
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Azure/ARO-HCP/test/pkg/configoverride"
	"github.com/Azure/ARO-HCP/test/pkg/leaseenv"
)

func DefaultOptions() *RawOptions {
	return &RawOptions{
		SourceDir:         ".",
		DeployEnvironment: os.Getenv(leaseenv.DeployEnvironment),
		BuildID:           os.Getenv("BUILD_ID"),
	}
}

func BindOptions(opts *RawOptions, cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&opts.SourceDir, "source-dir", opts.SourceDir, "Selected source checkout for relative paths")
	flags.StringVar(&opts.DeployEnvironment, "deploy-env", opts.DeployEnvironment, "Deployment environment")
	flags.StringVar(&opts.DevSettings, "dev-settings-file", opts.DevSettings, "Templatize dev settings that identify per-run environments (default source-dir/"+configoverride.DefaultDevSettingsFile+")")
}

type RawOptions struct {
	SourceDir         string
	DeployEnvironment string
	DevSettings       string
	BuildID           string
}

type Options struct {
	devSettings       string
	deployEnvironment string
	buildID           string
}

func NewCommand() *cobra.Command {
	opts := DefaultOptions()
	command := &cobra.Command{
		Use:   "run-cost-tag",
		Short: "Print the key=value tag that attributes a per-run environment's resources to its Prow job",
		Long: "Print the key=value cost attribution tag for resources this job creates, or nothing for an " +
			"environment that outlives a single job. The job ID is read from BUILD_ID.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			completed, err := opts.Complete()
			if err != nil {
				return err
			}
			return completed.Run(command.OutOrStdout())
		},
	}
	BindOptions(opts, command)
	return command
}

func (o *RawOptions) Complete() (*Options, error) {
	if o.DeployEnvironment == "" {
		return nil, fmt.Errorf("--deploy-env or %s is required", leaseenv.DeployEnvironment)
	}
	devSettings := o.DevSettings
	if devSettings == "" {
		devSettings = configoverride.DefaultDevSettingsFile
	}
	if !filepath.IsAbs(devSettings) {
		devSettings = filepath.Join(o.SourceDir, devSettings)
	}
	return &Options{devSettings: devSettings, deployEnvironment: o.DeployEnvironment, buildID: o.BuildID}, nil
}

func (o *Options) Run(out io.Writer) error {
	value, tagged, err := configoverride.RunCostTag(o.devSettings, o.deployEnvironment, o.buildID)
	if err != nil || !tagged {
		return err
	}
	_, err = fmt.Fprintf(out, "%s=%s\n", configoverride.RunCostTagKey, value)
	return err
}
