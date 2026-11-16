package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveRuntimePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		flag     string
		flagSet  bool
		env      string
		file     string
		want     string
		source   string
		invalid  bool
	}{
		{
			name:     "flag overrides both sources",
			platform: "darwin",
			flag:     "apple",
			flagSet:  true,
			env:      "docker",
			file:     "docker",
			want:     "apple",
			source:   "--runtime",
		},
		{
			name:     "environment overrides file",
			platform: "darwin",
			env:      "docker",
			file:     "apple",
			want:     "docker",
			source:   "SIDEKICKS_RUNTIME",
		},
		{
			name:     "file overrides macOS default",
			platform: "darwin",
			file:     "docker",
			want:     "docker",
			source:   "config file",
		},
		{
			name:     "macOS default",
			platform: "darwin",
			want:     "apple",
			source:   "platform default",
		},
		{
			name:     "Linux default",
			platform: "linux",
			want:     "docker",
			source:   "platform default",
		},
		{
			name:     "Linux rejects Apple",
			platform: "linux",
			flagSet:  true,
			flag:     "apple",
			invalid:  true,
		},
		{
			name:     "explicitly empty flag is invalid",
			platform: "darwin",
			flagSet:  true,
			file:     "docker",
			invalid:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := resolveRuntime(tc.platform, tc.flag, tc.flagSet, tc.env, tc.file)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, value.Value)
			require.Equal(t, tc.source, value.Source)
		})
	}
}

func TestConfigCommandsPersistOnlyTheFileValue(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", directory)
	t.Setenv("SIDEKICKS_RUNTIME", "")
	config := filepath.Join(directory, "sidekicks", "config.yaml")

	defaultRuntime := "docker"
	if runtime.GOOS == "darwin" {
		defaultRuntime = "apple"
	}
	require.Equal(t, defaultRuntime+"\n", executeCLI(t, "config", "get", "runtime"))
	require.NoFileExists(t, config)
	executeCLI(t, "config", "set", "runtime", "docker")
	file, err := os.ReadFile(config)
	require.NoError(t, err)
	require.Equal(t, "runtime: docker\n", string(file))
	require.Contains(t, executeCLI(t, "config", "show"), "config file")
	require.Contains(t, executeCLI(t, "config", "show"), config)

	t.Setenv("SIDEKICKS_RUNTIME", "invalid")
	require.Equal(t, "docker\n", executeCLI(t, "config", "get", "runtime", "--runtime", "docker"))
	executeCLI(t, "config", "unset", "runtime")
	file, err = os.ReadFile(config)
	require.NoError(t, err)
	require.NotContains(t, string(file), "runtime:")
	t.Setenv("SIDEKICKS_RUNTIME", "")
	require.Equal(t, defaultRuntime+"\n", executeCLI(t, "config", "get", "runtime"))
}

func TestStubsHaveHelpWithNoRuntimeRequired(t *testing.T) {
	t.Setenv("SIDEKICKS_RUNTIME", "invalid")
	require.Contains(t, executeCLI(t, "shell"), "Shell commands are reserved")
	require.Contains(t, executeCLI(t, "computer"), "Computer commands are reserved")
}

func executeCLI(t *testing.T, args ...string) string {
	t.Helper()
	var output bytes.Buffer
	root := newRoot()
	root.SetArgs(args)
	root.SetOut(&output)
	require.NoError(t, root.ExecuteContext(t.Context()), "%q", args)
	return output.String()
}
