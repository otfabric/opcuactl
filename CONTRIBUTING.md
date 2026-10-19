# Contributing to opcuactl

Thank you for your interest in contributing to `opcuactl`! This document covers the guidelines for contributing.

## Getting Started

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```sh
   git clone git@github.com:<your-username>/opcuactl.git
   cd opcuactl
   ```
3. Create a feature branch:
   ```sh
   git checkout -b feature/my-change
   ```

## Development Setup

### Prerequisites

- Go 1.26+
- [staticcheck](https://staticcheck.dev/) for linting
- Access to an OPC UA server for integration testing (optional)

### Build & Test

```sh
# Run all checks (format, lint, vet, test)
make check

# Run tests with race detector
make test

# Build for current platform (./bin/opcuactl)
make build
```

## Making Changes

### Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep functions focused and small
- Use meaningful variable and function names
- Add comments only where the logic isn't self-evident

### Commit Messages

Use clear, concise commit messages:

```
component: short description

Optional longer explanation of the change, why it was made,
and any relevant context.
```

Examples:
- `browse: add qualified path resolution`
- `format: fix array value serialization`
- `docs: update EXAMPLES.md with v0.1.10 outputs`

### Testing

- Add tests for new functionality in the appropriate `_test.go` file
- Run `make check` before submitting to ensure all checks pass
- If your change affects command output, verify against a real OPC UA server when possible

## Submitting Changes

1. Push your branch to your fork:
   ```sh
   git push origin feature/my-change
   ```
2. Open a Pull Request against the `main` branch
3. Describe what your change does and why
4. Reference any related issues

## Reporting Issues

- Use GitHub Issues to report bugs or suggest features
- Include the output of `opcuactl version` in bug reports
- Provide the exact command that triggered the issue, along with the error output
- Mention the OPC UA server type/vendor if relevant

## Project Structure

```
cmd/opcuactl/          Command implementations (one file per command group)
internal/conn/         Client connection management and auth/security options
internal/format/       Output formatting (table, JSON, YAML)
internal/model/        Output data models
internal/parse/        NodeID parsing, type coercion, security policy handling
```

## License

By contributing to `opcuactl`, you agree that your contributions will be licensed under the [MIT License](LICENSE).
