# Sidekicks CLI design

Sidekicks gives an AI agent a browser it can start, inspect, and drive independently of the user's browser. The first client is a coding agent such as Codex invoking the CLI. The same Go client and guest API should support a custom agent harness later.

The first release covers named browser instances on macOS and Linux, with ordinary outbound connectivity and a host-local control endpoint. `sidekicks shell` and `sidekicks computer` are help-only command groups across the iterations in this design. Their subcommands, capabilities, and images are deferred. The eventual computer workload is a persistent desktop where a user or agent can install Linux apps and control them with a mouse and keyboard.

## Runtime selection and configuration

Use Cobra for the command tree under `cmd/sidekicks`. Keep `main.go` focused on wiring and passing the process context to the root command. Register `--runtime` as a persistent root flag and check whether the caller supplied it when resolving runtime precedence. [Cobra user guide](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md)

Resolve the runtime once for each invocation, in this order.

1. The global `--runtime` flag.
2. `SIDEKICKS_RUNTIME`.
3. The `runtime` key in the config file.
4. The platform default.

On macOS the default is `apple` and the alternative is `docker`. On Linux the only supported value is `docker`. The Apple backend uses Apple's `container` CLI on Apple silicon with macOS 26 or later. If the selected runtime is unavailable, report the setup step or the explicit Docker override on macOS. [Apple `container` requirements](https://github.com/apple/container#requirements)

Use `$XDG_CONFIG_HOME/sidekicks/config.yaml`, falling back to `~/.config/sidekicks/config.yaml` when the variable is unset, empty, or relative. When the file is absent, resolve settings from the remaining sources. [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/)

| Command | Behavior |
| --- | --- |
| `sidekicks config show` | Print the config path and effective settings with their sources. |
| `sidekicks config get <key>` | Print the effective value alone. On macOS, `get runtime` prints `apple` when the platform default applies. |
| `sidekicks config set <key> <value>` | Validate the key and value and write the persisted setting. |
| `sidekicks config unset <key>` | Remove the persisted setting. A flag or environment override still applies for that invocation. |

Names are scoped to the selected backend. `list`, `start`, `stop`, `delete`, and capability commands all use that backend. Include the runtime in lookup errors and scope any local instance metadata to the backend and instance so `web` can exist in both runtimes. For example, after `sidekicks config set runtime docker`, `sidekicks browser stop --name web` targets Docker. `sidekicks --runtime apple browser stop --name web` targets Apple `container` for that command.

## Instance lifecycle

| Command | Behavior |
| --- | --- |
| `sidekicks browser start --name <name>` | Create a fresh instance if the name is absent, restart a stopped instance, or reuse a running instance. Return once the guest API and browser are ready. |
| `sidekicks browser stop --name <name>` | Shut down the workload and retain the name and its on-disk state, including the profile, cookies, and downloads stored in the instance. |
| `sidekicks browser delete --name <name>` | Remove the instance and its Sidekicks-owned storage. A subsequent `start` creates a fresh instance. |
| `sidekicks list` | Show running and stopped Sidekicks instances in the selected runtime. |

Chromium starts again on restart, with a fresh set of live tab IDs. Use runtime-owned container and storage identifiers for lifecycle operations and verify Sidekicks ownership before acting on a match. Both runtimes can restart stopped containers. [Apple `container start`](https://github.com/apple/container/blob/main/docs/command-reference.md#container-start) · [Docker `start`](https://docs.docker.com/reference/cli/docker/container/start/)

## Browser commands and tab targeting

```sh
sidekicks browser start --name web
sidekicks browser cmd --name web goto https://example.com
sidekicks browser cmd --name web snapshot
sidekicks browser tabs --name web
sidekicks browser cdp --name web Browser.getVersion
sidekicks browser screenshot --name web -o page.png
sidekicks browser view --name web
sidekicks browser stop --name web
sidekicks browser delete --name web
```

- `browser cmd` starts with typed `goto`, `snapshot`, `click`, and `type` operations. A snapshot gives a coding agent structured page content and enough information to choose an interaction. Reuse the pinned Stagehand Go SDK where it supports these operations. Add Playwright when a specific operation needs it. [Stagehand integration](../infrastructure/images/browser-use/browser_test.go#L45) · [Playwright CDP connection](https://playwright.dev/docs/api/class-browsertype#browser-type-connect-over-cdp)
- `browser cdp` executes a named Chrome DevTools Protocol (CDP) method with optional JSON parameters. Browser-scope methods operate on the browser connection. Page-scope methods use the selected tab. Treat raw CDP as operator access when delegated credentials are introduced.
- `browser screenshot -o <path>` captures the selected page and writes the image on the machine running the CLI. Machine-readable command results go to stdout and diagnostics go to stderr.
- `browser view` opens an interactive viewer on the machine running the CLI. The viewer displays and sends input to the selected tab so a person can complete a login or take over a manual step.

Each browser begins with a primary tab created by the guest. `browser cmd`, page-scoped `browser cdp`, `browser screenshot`, and `browser view` use it by default. `browser tabs` returns the live tab IDs, and `--tab <id>` selects an explicit tab. Window focus and new popups leave the primary tab selection intact. If the selected tab closes, return a tab error and let the caller choose a live ID. Tab IDs belong to a running browser session.

## Go client and guest process

The Go client uses small lifecycle adapters for Apple `container` and Docker. Invoke the CLIs directly with `exec.CommandContext` and argument vectors. Read machine-readable output for discovery and inspection. The Docker adapter follows the Docker CLI's active context so Docker Desktop works with the same configuration as the user's `docker` commands. The Apple adapter uses the installed `container` service through its CLI. Reconsider Docker's Go SDK or a Swift bridge for Apple when a required operation or embedding target calls for direct integration. [Apple command reference](https://github.com/apple/container/blob/main/docs/command-reference.md) · [Apple Containerization](https://github.com/apple/containerization) · [Docker contexts](https://docs.docker.com/engine/manage-resources/contexts/) · [Docker Go SDK](https://docs.docker.com/reference/api/engine/sdk/)

Both adapters launch the same browser-use OCI image and give the client a host-local guest endpoint. Docker publishes a TCP port on host loopback. Apple publishes a Unix socket in a private host directory through its native socket relay. `internal/clients/browser` owns the Go client for the guest API and serves both the CLI and the future harness.

`sidekicks-guest` starts Chromium with its managed profile and existing sandbox configuration, waits for CDP, initializes the primary tab, serves the guest API, and shuts Chromium down when the instance stops. It embeds the browser gateway with bearer authentication and keeps Chromium's raw CDP listener on guest loopback. Local startup provisions the gateway token internally. User-facing credential management is a later iteration. [Guest entrypoint](../cmd/sidekicks-guest/main.go) · [Browser gateway](../internal/services/browsergateway/gateway.go)

The separate Chromium native-messaging helper becomes `wba-bridge` in the Web Bot Auth (WBA) iteration. It relays messages to the signing broker. The future `sidekicks-controller` reconciles instance lifecycles across Kubernetes. [Existing WBA bridge](../cmd/wba-native-host/main.go) · [OpenShell Kubernetes architecture](https://docs.nvidia.com/openshell/kubernetes/setup)

## Implementation iterations

### 1. Local browser for coding agents

Implement config resolution, the two runtime adapters, browser lifecycle, the guest entrypoint, tab targeting, CDP, typed browser commands, and screenshots. Register the `shell` and `computer` stubs. Leave outbound networking to the runtime's ordinary configuration.

Accept the iteration when a coding agent can start two named browsers, navigate, inspect a page, enter text, click, and save a screenshot. Stop one instance, verify that the other still works, then restart the stopped instance and verify that its profile data persists. Delete it and confirm the next start has fresh state. On macOS, create the same name in both backends and verify that each command resolves to the runtime selected by config or override. Exercise the flow on macOS with both backends and on Linux with Docker.

Update the existing Kubernetes dev deployment and port-forward for the guest listener. Keep the browser smoke test and the WBA fixture available through the entrypoint change. [Existing dev setup](../infrastructure/images/browser-use/README.md#local-kubernetes-development)

### 2. Browser viewer

Serve the viewer from a temporary loopback address on the CLI machine. The foreground command prints the address and opens the default browser. Pass `--no-open` to open the printed link yourself. The link is valid while the command runs. Press Ctrl-C in the CLI to end the viewer session and keep the guest running.

The CLI keeps the guest bearer token and relays display and input over the existing runtime connection. The guest streams CDP screencast frames for the selected tab and captures screenshots when Chromium pauses the screencast. The viewer sends mouse, scroll, and keyboard events to that tab.

Accept when a human can complete a manual interaction and the agent can inspect the result on the same tab. Switching focus to another tab must leave the agent's target intact. Closing the selected tab ends its viewer stream.

### 3. Policy, WBA, and credentials

These are fast follows to the initial browser release. The viewer can ship independently.

- **Policy.** Add `sidekicks policy create -o <path>`, `sidekicks policy validate <path>`, and `browser start --policy <path>`. Use versioned, OpenShell-inspired YAML for filesystem access, network egress, and WBA signing. Choose the policy defaults and decide whether the first network rules need HTTP method and path matching. Validate and pin the policy at launch. Have the runtime enforce filesystem constraints and direct egress restrictions, with an egress proxy evaluating allowed traffic. Test that policy-denied traffic fails to reach its destination on each supported backend. [OpenShell policy model](https://docs.nvidia.com/openshell/dev/how-it-works/policies/overview)
- **WBA.** Keep private keys in a signing broker outside the workload and reference signing identities from policy. Prepare a neutral blank tab and wait for signing readiness before managed signed navigation. The current integration test covers the blank-tab flow, redirect handling, and broker timeout. Decide the first signing guarantee before release. Managed top-level HTTPS GET and HEAD fit the current bridge. Requiring signatures on every request to an origin, including subresources and requests initiated through raw CDP or the viewer, requires a trusted HTTP enforcement point that can inspect the signature headers. [Existing WBA integration](../infrastructure/images/browser-use/wba_test.go#L21)
- **Credentials.** Define which callers can use typed browser commands, raw CDP, and the viewer. Keep the WBA signing identity distinct from credentials used to access the guest API.

### 4. Kubernetes hosting

Add a controller that reconciles named instances and reports readiness. Each browser image runs `sidekicks-guest`. Define remote discovery, authentication, and transport for the guest API and viewer together. If the CLI exposes a local `--listen` override, it controls the host-side bind. Define cluster exposure alongside the remote access workflow.
