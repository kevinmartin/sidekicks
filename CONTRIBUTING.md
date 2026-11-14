# Contributing to Sidekicks

Sidekicks uses one root Go module for its Go commands and packages. Follow the repository instructions for Go style and review conventions. [Go module](go.mod) · [Repository instructions](AGENTS.md)

## Repository layout

Use this layout when adding packages or moving code into its owning package.

| Path | What belongs there |
| --- | --- |
| `cmd/<binary>/` | Product entry points, startup and shutdown, config loading, dependency wiring, and command-specific route registration. |
| `internal/services/<service>/` | Service behavior, transport handlers and their request and response types, and service-owned data access. |
| `internal/domain/<capability>/` | Sidekicks business rules with a lifecycle independent of a service or transport, and the types those rules operate on. |
| `internal/clients/<provider>/` | Reusable clients for external APIs such as GitHub and Slack. Services own the decisions about when to call them. |
| `internal/harness/` | The future Sidekicks agent harness, including execution and tool orchestration. |
| `internal/lib/<library>/` | Reusable protocol and infrastructure packages, such as Web Bot Auth and shared database connection setup. |
| `extensions/sidekicks/` | The Sidekicks Chrome extension. Web Bot Auth is one feature module. |
| `infrastructure/images/<image>/` | Dockerfiles, image-specific tools and fixtures, and tests for the image in a deployed stack. |
| `infrastructure/kubernetes/` | Kubernetes manifests and Kustomize configuration. |

Update imports, Dockerfile source paths, and any path-specific test commands together when moving or adding a Go package used by an image. [Dev-tools Dockerfile](infrastructure/images/browser-dev-tools/Dockerfile)

## Service boundaries

The browser gateway's command registers method and path patterns and attaches middleware. The service package implements the handlers. Use `http.ServeMux` when its patterns cover the routing needs. For a larger API with its own router, assemble it in the service's transport package and mount it from the command. [Gateway entry point](cmd/browser-gateway/main.go) · [Gateway handlers](internal/services/browsergateway/gateway.go)

Start with files within the owning service package. Separate handlers from service decisions so those decisions can be tested directly. Split out a package such as `httpapi` or `postgres` when that gives the adapter a clear interface or isolates its dependencies. The service defines the store interface when it needs one, and the command wires the implementation. [Go interface guidance](https://go.dev/wiki/CodeReviewComments#interfaces)

Keep a business type with its rules, HTTP request and response types with the handlers that use them, and database row types with their queries. Put shared business types in a named capability package under `internal/domain/`. A shared `internal/lib/db` package can own connection and pool setup. Service-specific queries and conversions live with the owning service.

### Signing policy

The dev fixture currently selects which requests to sign. It uses `internal/lib/webbotauth` for signature generation. The library also owns key thumbprints and signed directory responses. [Fixture broker](infrastructure/images/browser-dev-tools/wba-fixture/broker.go) · [Web Bot Auth](internal/lib/webbotauth/README.md)

When implementing dynamic signing policies, put the policy type, validation, and deterministic request matching in `internal/domain/signingpolicy`. The evaluator uses only policy and request data. The planned `internal/services/signingbroker` package will apply the session policy and call the signer for matching requests.

## Local development and CI

- Use `make test` for Go unit tests. Keep tests beside the code they exercise.
- Use `make dev` to build the images, deploy the Docker Desktop Kubernetes stack, and forward local ports. Run `make integration` from a second terminal. Browser tests connect to the running deployment. The repo-wide `integration` suite also runs the live Web Bot Auth checks with the prerequisites documented in the browser image README. [Makefile](Makefile) · [Browser development setup](infrastructure/images/browser-use/README.md#local-kubernetes-development)
- Keep build, deploy, and port-forward steps independently callable in Make. Place each `.PHONY` declaration immediately above its target with the colons aligned, and use file targets for generated files with tracked inputs.
- PRs and pushes to `main` share checks for Go, the extension, Kubernetes manifests, and container images. CI builds the images and deploys them to a temporary kind cluster before running the browser integration tests. [Reusable checks](.github/workflows/checks.reusable.yml)

Keep component setup instructions and diagrams beside the component. When planning an architecture change, list the paths to add, change, or remove and identify the diagrams affected by the change.
