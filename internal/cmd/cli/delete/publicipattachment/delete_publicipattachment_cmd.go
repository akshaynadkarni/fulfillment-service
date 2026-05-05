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
		Use:   "publicipattachment PUBLIC_IP",
		Short: "Detach a public IP from its compute instance",
		Long: "Detach an existing public IP from the compute instance it is currently attached to. " +
			"The public IP is identified by its ID or name.",
		Example: `  # Detach a public IP by name
  osac delete publicipattachment my-ip

  # Detach a public IP by ID
  osac delete publicipattachment pip-abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runner.run,
	}
	return result
}

type runnerContext struct {
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
		Id: args[0],
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to get public IP '%s': %w", args[0], err)
	}

	pip := getResponse.GetObject()
	if spec := pip.GetSpec(); spec != nil {
		spec.ClearComputeInstance()
	}

	response, err := client.Update(ctx, publicv1.PublicIPsUpdateRequest_builder{
		Object: pip,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"spec.compute_instance"},
		},
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to detach public IP: %w", err)
	}

	c.console.Infof(ctx, "Detached public IP '%s' from its compute instance.\n",
		response.GetObject().GetId())

	return nil
}
