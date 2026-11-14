# Sidekicks repository instructions

## Package placement

- Keep each service's behavior, handlers, and data access together under `internal/services/<service>/`. Put shared Sidekicks business rules in `internal/domain/` and reusable protocol and infrastructure code in `internal/lib/`. See the [repository layout](CONTRIBUTING.md#repository-layout).
- Keep `cmd/<binary>/main.go` focused on configuration, dependency wiring, and process lifecycle. Register command-specific routes and middleware in `cmd/<binary>/`.
- When implementing dynamic signing policies, put the policy type, validation, and matching in `internal/domain/signingpolicy`. Put the service that applies the session policy and calls the Web Bot Auth signer in `internal/services/signingbroker`.

## Go style

- Run `gofmt` on changed Go files. Use parenthesized multiline import blocks even for a single import. Group standard library imports first, with a blank line before other imports.
- Use multiline bodies for function declarations and literals. Put each entry of a keyed struct or map literal on its own line and each argument of a long call on its own line.
- Keep the happy path left-aligned with early returns. Prefer explicit return values, including in functions with named results.
- Document exported API names with complete, punctuated sentences that begin with the name. Write prose comments as complete sentences. Give tests descriptive names and explain non-obvious setup or invariants. Preserve the required syntax of Go directives and generated-code markers.
- Handle errors and add operation context where it helps the caller. Use `%w` when callers should be able to inspect the cause with `errors.Is` or `errors.As`. Start error text lowercase except for names and identifiers that require specific casing. Refer to the argument at the package boundary and the environment variable at the configuration boundary.
- Put `ctx context.Context` first in functions that accept a context, including test helpers. Propagate request cancellation and deadlines through I/O. HTTP handlers use the request's context.
- Set up cleanup immediately after acquiring a resource the function owns. Defer closing files, response bodies, and SQL rows when their lifetime matches the function scope. Defer rollback after beginning a transaction and close shared database pools during service shutdown. Parameterize SQL values.
- Give each goroutine an owner, an exit path, and a way to report failure. Recover and report panics at boundaries where the operation can safely fail on its own. `net/http` already recovers panics from `ServeHTTP`. Let a critical worker panic stop the process for restart. Synchronize shared mutable state with a mutex or another explicit ownership mechanism.
- Load deployment settings into typed config structs at startup using `caarlos0/env/v11` and validate them before wiring the service. Keep protocol constants and deliberate defaults in code. Prefer modern Go idioms supported by the `go.mod` language version.

Use Go's code review guidance for conventions beyond these repository rules. [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)

## Tests

- Use table-driven tests when cases share the same setup and assertions. Prefer Testify when it makes assertions clearer. Use fakes, mocks, or `httptest` for external dependencies in unit tests.
- Synchronize with channels, wait groups, or bounded polling. Use timers to exercise behavior that depends on elapsed time.
- Preserve byte-level contracts when changing signed or hashed payloads. Cover the expected encoding with regression tests.
- Run `make test` for Go unit tests. Browser integration tests use the running deployment provisioned by Make or CI. See [local development and CI](CONTRIBUTING.md#local-development-and-ci) for the full workflow.
