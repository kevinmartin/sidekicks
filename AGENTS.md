# Repository instructions

## Go conventions

- Run `gofmt` and keep every import declaration in multiline parentheses, including a single import.
- Give every function declaration and function literal a multiline body.
- Document exported functions, methods, variables and structs with a sentence that begins with the exported name and ends with punctuation. Include test functions and methods on unexported types.
- Write prose comments as complete sentences with punctuation. Preserve Go directives, build tags, generated-code markers, URLs and code examples in their required technical form.
- Use lowercase error text. Preserve case-sensitive identifiers, protocol tokens and quoted external errors when changing them would be misleading or incorrect.
- Prefer modern Go idioms when they improve the code without changing behavior or the module's language compatibility. The `go.mod` directive is the language baseline; use a currently supported, patched Go toolchain.

`make check` enforces `gofmt`, `go vet`, Staticcheck, tests, coverage and compilation. The additional conventions above are review instructions; these tools do not enforce all of them.
