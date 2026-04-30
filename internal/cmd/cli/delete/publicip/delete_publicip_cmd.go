/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicip

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/exit"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "publicip [flags] ID_OR_NAME...",
		Aliases: []string{"publicips"},
		Short:   "Delete public IPs",
		Long:    "Delete one or more public IPs, identified by ID or name. The allocated address is returned to the parent pool.",
		Example: `  # Delete a public IP by ID
  osac delete publicip pip-abc123

  # Delete a public IP by name
  osac delete publicip my-ip

  # Delete multiple public IPs
  osac delete publicip my-ip-1 my-ip-2`,
		Args: cobra.MinimumNArgs(1),
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

	quoted := make([]string, len(args))
	for i, ref := range args {
		quoted[i] = strconv.Quote(ref)
	}
	joined := strings.Join(quoted, ", ")
	filter := fmt.Sprintf(`this.id in [%[1]s] || this.metadata.name in [%[1]s]`, joined)
	listResponse, err := client.List(ctx, publicv1.PublicIPsListRequest_builder{
		Filter: &filter,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to list public IPs: %w", err)
	}

	matches := make(map[string][]string, len(args))
	for _, item := range listResponse.GetItems() {
		id := item.GetId()
		name := item.GetMetadata().GetName()
		for _, ref := range args {
			if id == ref || name == ref {
				matches[ref] = append(matches[ref], id)
			}
		}
	}

	// Validate all references before deleting anything
	ids := make([]string, 0, len(args))
	for _, ref := range args {
		switch len(matches[ref]) {
		case 0:
			return fmt.Errorf("public IP not found: %s", ref)
		case 1:
			ids = append(ids, matches[ref][0])
		default:
			return fmt.Errorf("multiple public IPs match '%s', use the ID instead", ref)
		}
	}

	// Delete each resolved public IP. Attempt all deletions and report errorsat the end
	var hadErrors bool
	for i, id := range ids {
		_, err = client.Delete(ctx, publicv1.PublicIPsDeleteRequest_builder{Id: id}.Build())
		if err != nil {
			hadErrors = true
			c.console.Errorf(ctx, "Failed to delete public IP '%s': %v\n", args[i], err)
			continue
		}
		c.console.Infof(ctx, "Deleted public IP '%s'.\n", args[i])
	}
	if hadErrors {
		return exit.Error(1)
	}

	return nil
}
