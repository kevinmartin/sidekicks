package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kevinmartin/sidekicks/internal/clients/browser"
	"github.com/kevinmartin/sidekicks/internal/domain/browserinstance"
	"github.com/kevinmartin/sidekicks/internal/lib/containerruntime"
	"github.com/spf13/cobra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := newRoot().ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "sidekicks:", err)
		os.Exit(1)
	}
}

type options struct {
	runtime string
}

func newRoot() *cobra.Command {
	var opts options
	root := &cobra.Command{
		Use:           "sidekicks",
		Short:         "Manage browser instances for agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&opts.runtime, "runtime", "", "Container runtime (apple or docker)")
	root.AddCommand(
		newConfigCommand(&opts),
		newBrowserCommand(&opts),
		&cobra.Command{
			Use:   "list",
			Short: "List browser instances in the selected runtime",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				manager, err := opts.manager(cmd)
				if err != nil {
					return err
				}
				instances, err := manager.List(cmd.Context())
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(instances)
			},
		},
		stub("shell", "Shell commands are reserved for a future release"),
		stub("computer", "Computer commands are reserved for a future release"),
	)
	return root
}

func stub(name, description string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}

func (o *options) manager(cmd *cobra.Command) (*browserinstance.Manager, error) {
	_, selected, err := o.effectiveConfig(cmd)
	if err != nil {
		return nil, err
	}
	backend, err := containerruntime.Open(cmd.Context(), selected.Value, os.Getenv("SIDEKICKS_BROWSER_IMAGE"))
	if err != nil {
		return nil, err
	}
	return browserinstance.New(backend, func(ctx context.Context, instance browserinstance.Instance) error {
		_, err := browser.New(instance.Endpoint, instance.Token).Tabs(ctx)
		return err
	}), nil
}
