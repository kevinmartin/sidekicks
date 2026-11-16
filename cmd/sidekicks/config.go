package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

type fileConfig struct {
	Runtime string `yaml:"runtime,omitempty"`
}

type configValue struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

func configPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for Sidekicks config: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "sidekicks", "config.yaml"), nil
}

func loadConfig(path string) (fileConfig, error) {
	var cfg fileConfig
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && err != io.EOF {
		return cfg, fmt.Errorf("decode config %s: %w", path, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return cfg, fmt.Errorf("config %s must contain a single YAML document", path)
	}
	return cfg, nil
}

func resolveRuntime(platform, flagValue string, flagSet bool, envValue, fileValue string) (configValue, error) {
	var result configValue
	switch {
	case flagSet:
		result = configValue{
			Value:  flagValue,
			Source: "--runtime",
		}
	case envValue != "":
		result = configValue{
			Value:  envValue,
			Source: "SIDEKICKS_RUNTIME",
		}
	case fileValue != "":
		result = configValue{
			Value:  fileValue,
			Source: "config file",
		}
	default:
		result = configValue{
			Value:  "docker",
			Source: "platform default",
		}
		if platform == "darwin" {
			result.Value = "apple"
		}
	}
	return result, validateRuntime(platform, result.Value, result.Source)
}

func validateRuntime(platform, value, source string) error {
	switch platform {
	case "darwin":
		if value != "apple" && value != "docker" {
			return fmt.Errorf("%s runtime %q is invalid on macOS; use apple or docker", source, value)
		}
	case "linux":
		if value != "docker" {
			return fmt.Errorf("%s runtime %q is invalid on Linux; use docker", source, value)
		}
	default:
		return fmt.Errorf("platform %s is unsupported; use macOS or Linux", platform)
	}
	return nil
}

func (o *options) effectiveConfig(cmd *cobra.Command) (string, configValue, error) {
	path, err := configPath()
	if err != nil {
		return "", configValue{}, err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return "", configValue{}, err
	}
	selected, err := resolveRuntime(runtime.GOOS, o.runtime, cmd.Flags().Changed("runtime"), os.Getenv("SIDEKICKS_RUNTIME"), cfg.Runtime)
	return path, selected, err
}

func newConfigCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Read and write user configuration",
	}
	command.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show the config path and effective settings",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				path, value, err := opts.effectiveConfig(cmd)
				if err != nil {
					return err
				}
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(struct {
					Path    string      `json:"path"`
					Runtime configValue `json:"runtime"`
				}{
					Path:    path,
					Runtime: value,
				})
			},
		},
		&cobra.Command{
			Use:   "get <key>",
			Short: "Print the effective value of a setting",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := validateConfigKey(args[0]); err != nil {
					return err
				}
				_, value, err := opts.effectiveConfig(cmd)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), value.Value)
				return err
			},
		},
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Persist a setting",
			Args:  cobra.ExactArgs(2),
			RunE: func(_ *cobra.Command, args []string) error {
				if err := validateConfigKey(args[0]); err != nil {
					return err
				}
				if err := validateRuntime(runtime.GOOS, args[1], "config file"); err != nil {
					return err
				}
				return writeConfig(args[1])
			},
		},
		&cobra.Command{
			Use:   "unset <key>",
			Short: "Remove a persisted setting",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				if err := validateConfigKey(args[0]); err != nil {
					return err
				}
				return writeConfig("")
			},
		},
	)
	return command
}

func validateConfigKey(key string) error {
	if key != "runtime" {
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}

func writeConfig(value string) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if _, err := loadConfig(path); err != nil {
		return err
	}
	data, err := yaml.Marshal(fileConfig{
		Runtime: value,
	})
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := writeFile(path, data); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

func writeFile(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".sidekicks-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(file.Name())
	}()
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
