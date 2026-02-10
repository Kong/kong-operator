# AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) and other AI agents when working with code in this repository.

## Project Overview

This project generates Kubernetes Custom Resource Definitions (CRDs) from OpenAPI Specifications (OAS).
It includes tools for code generation, linting, and testing to ensure high-quality code and maintainability.

## Build commands

```bash
mise r build            # Build the manager binary (includes code generation)
```

## Generation

```bash
mise r generate-api     # Generate Kubernetes CRDs from OpenAPI Specifications
```

## Linting

```bash
mise r lint             # Run Go linters (modules, golangci-lint, modernize)
mise r lint.api         # Lint Kubernetes API types
```

## Testing

### Unit Tests

```bash
mise r test-unit        # Run unit tests with verbose output
```

## Instructions

- Prefer to use `mise r generate-api` to generate Kubernetes CRDs from OpenAPI spec
  rather than building the project with `mise r build` to avoid unnecessary compilation of the manager binary.
- To run the whole pipeline from generation, through linting and testing, use:
  ```
  mise r generate-api ; mise r lint ::: lint-api ::: test-unit
  ```
