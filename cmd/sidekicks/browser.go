package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kevinmartin/sidekicks/internal/clients/browser"
	"github.com/kevinmartin/sidekicks/internal/domain/browserinstance"
	"github.com/kevinmartin/sidekicks/internal/lib/browserapi"
	"github.com/spf13/cobra"
)

type browserOptions struct {
	*options
	name string
}

func newBrowserCommand(opts *options) *cobra.Command {
	browser := &browserOptions{
		options: opts,
	}
	command := &cobra.Command{
		Use:   "browser",
		Short: "Manage and control named browsers",
	}
	command.PersistentFlags().StringVar(&browser.name, "name", "", "Browser instance name")
	command.AddCommand(
		browser.lifecycle("start", "Start or reuse a browser and wait for readiness"),
		browser.lifecycle("stop", "Stop a browser and keep its profile"),
		browser.lifecycle("delete", "Delete a browser and its profile"),
		browser.typedCommands(),
		browser.cdpCommand(),
		browser.screenshotCommand(),
		&cobra.Command{
			Use:   "tabs",
			Short: "List live tab IDs and the primary tab",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				client, err := browser.client(cmd)
				if err != nil {
					return err
				}
				tabs, err := client.Tabs(cmd.Context())
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(tabs)
			},
		},
	)
	return command
}

func (o *browserOptions) instanceManager(cmd *cobra.Command) (*browserinstance.Manager, error) {
	if o.name == "" {
		return nil, errors.New("--name is required")
	}
	return o.manager(cmd)
}

func (o *browserOptions) client(cmd *cobra.Command) (*browser.Client, error) {
	manager, err := o.instanceManager(cmd)
	if err != nil {
		return nil, err
	}
	instance, err := manager.Running(cmd.Context(), o.name)
	if err != nil {
		return nil, err
	}
	return browser.New(instance.Endpoint, instance.Token), nil
}

func (o *browserOptions) lifecycle(operation, summary string) *cobra.Command {
	return &cobra.Command{
		Use:   operation,
		Short: summary,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, err := o.instanceManager(cmd)
			if err != nil {
				return err
			}
			var result browserinstance.Instance
			switch operation {
			case "start":
				result, err = manager.Start(cmd.Context(), o.name)
			case "stop":
				result, err = manager.Stop(cmd.Context(), o.name)
			case "delete":
				return manager.Delete(cmd.Context(), o.name)
			}
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
}

func (o *browserOptions) typedCommands() *cobra.Command {
	var tab string
	command := &cobra.Command{
		Use:   "cmd",
		Short: "Run typed browser operations",
	}
	command.PersistentFlags().StringVar(&tab, "tab", "", "Target tab ID (default is the primary tab)")
	for _, op := range []struct {
		name    string
		usage   string
		summary string
		args    int
	}{
		{
			name:    "goto",
			usage:   "goto <url>",
			summary: "Navigate to a URL",
			args:    1,
		},
		{
			name:    "snapshot",
			usage:   "snapshot",
			summary: "Read the page tree and locator map",
			args:    0,
		},
		{
			name:    "click",
			usage:   "click <selector>",
			summary: "Click an element selected by CSS or XPath",
			args:    1,
		},
		{
			name:    "type",
			usage:   "type <selector> <text>",
			summary: "Fill an input with text",
			args:    2,
		},
	} {
		command.AddCommand(&cobra.Command{
			Use:   op.usage,
			Short: op.summary,
			Args:  cobra.ExactArgs(op.args),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := o.client(cmd)
				if err != nil {
					return err
				}
				request := browserapi.Command{
					Tab:       tab,
					Operation: op.name,
				}
				switch op.name {
				case "goto":
					request.URL = args[0]
				case "click":
					request.Selector = args[0]
				case "type":
					request.Selector = args[0]
					request.Text = args[1]
				}
				result, err := client.Command(cmd.Context(), request)
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			},
		})
	}
	return command
}

func (o *browserOptions) cdpCommand() *cobra.Command {
	var tab string
	var browserScope bool
	command := &cobra.Command{
		Use:   "cdp <Domain.method> [params]",
		Short: "Execute one Chrome DevTools Protocol command",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			request := browserapi.CDPCommand{
				Tab:     tab,
				Browser: browserScope,
				Method:  args[0],
			}
			if len(args) == 2 {
				value := strings.TrimSpace(args[1])
				if !strings.HasPrefix(value, "{") || !json.Valid([]byte(value)) {
					return errors.New("CDP params must be a JSON object")
				}
				request.Params = json.RawMessage(value)
			}
			client, err := o.client(cmd)
			if err != nil {
				return err
			}
			result, err := client.CDP(cmd.Context(), request)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	command.Flags().StringVar(&tab, "tab", "", "Target tab ID (default is the primary tab for page commands)")
	command.Flags().BoolVar(&browserScope, "browser", false, "Run the command on the browser connection")
	return command
}

func (o *browserOptions) screenshotCommand() *cobra.Command {
	var tab, path string
	command := &cobra.Command{
		Use:   "screenshot",
		Short: "Save a PNG screenshot on this machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if path == "" {
				return errors.New("--output is required")
			}
			client, err := o.client(cmd)
			if err != nil {
				return err
			}
			data, err := client.Screenshot(cmd.Context(), tab)
			if err != nil {
				return err
			}
			if err := writeFile(path, data); err != nil {
				return fmt.Errorf("save screenshot %s: %w", path, err)
			}
			return nil
		},
	}
	command.Flags().StringVar(&tab, "tab", "", "Target tab ID (default is the primary tab)")
	command.Flags().StringVarP(&path, "output", "o", "", "Output PNG path")
	return command
}
