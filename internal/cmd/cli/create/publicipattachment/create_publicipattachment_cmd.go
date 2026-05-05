/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicipattachment

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:   "publicipattachment [flags]",
		Short: "Attach a public IP to a compute instance",
		Long: "Attach an existing public IP to a compute instance by setting spec.compute_instance. " +
			"Both --publicip and --compute-instance flags are required.",
		Example: `  # Attach a public IP to a compute instance
  osac create publicipattachment --publicip my-ip --compute-instance my-vm

  # Attach using IDs
  osac create publicipattachment --publicip pip-abc123 --compute-instance ci-xyz789`,
		Args: cobra.NoArgs,
		RunE: runner.run,
	}
	flags := result.Flags()
	flags.StringVar(
		&runner.args.publicIP,
		"publicip",
		"",
		"ID of the public IP to attach.",
	)
	flags.StringVar(
		&runner.args.computeInstance,
		"compute-instance",
		"",
		"ID of the compute instance to attach the public IP to.",
	)
	result.MarkFlagRequired("publicip")         //nolint:errcheck
	result.MarkFlagRequired("compute-instance") //nolint:errcheck
	return result
}

type runnerContext struct {
	args struct {
		publicIP        string
		computeInstance string
	}
	logger  *slog.Logger
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg.Address == "" {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	conn, err := cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer conn.Close()

	client := publicv1.NewPublicIPsClient(conn)

	getResponse, err := client.Get(ctx, publicv1.PublicIPsGetRequest_builder{
		Id: c.args.publicIP,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to get public IP '%s': %w", c.args.publicIP, err)
	}

	pip := getResponse.GetObject()
	spec := pip.GetSpec()
	if spec == nil {
		spec = new(publicv1.PublicIPSpec)
		pip.SetSpec(spec)
	}
	spec.SetComputeInstance(c.args.computeInstance)

	response, err := client.Update(ctx, publicv1.PublicIPsUpdateRequest_builder{
		Object: pip,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"spec.compute_instance"},
		},
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to attach public IP: %w", err)
	}

	c.console.Infof(ctx, "Attached public IP '%s' to compute instance '%s'.\n",
		response.GetObject().GetId(), c.args.computeInstance)

	return nil
}
