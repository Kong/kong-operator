# AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) and other AI agents when working with code in this repository.

## Project Overview

This project generates Go types for Kubernetes Custom Resource Definitions (CRDs) from OpenAPI Specifications (OAS).
It includes tools for code generation, linting, and testing to ensure high-quality code and maintainability.

## Generation

```bash
mise r generate-api      # Generate Go types from OpenAPI Specifications
mise r generate-crds     # Generate Kubernetes CRD YAML manifests from generated Go types
mise r generate-deepcopy # Generate deepcopy methods
```

## Linting

```bash
mise r lint
```

## Testing

### Unit Tests

```bash
mise r test-unit
```

### CRD Validation Tests

```bash
mise r test-crdsvalidation # Run tests to validate generated CRDs against Kubernetes API server using envtest
```

## Instructions

- Prefer to use `mise r generate-api` to generate Kubernetes API types from OpenAPI Specifications
  and `mise r generate-crds` to generate CRD YAML manifests,
  rather than building the project with `mise r build` to avoid unnecessary compilation of the manager binary.
- To run the whole pipeline from generation, through linting and testing, use:
  ```
  mise r generate && mise r lint && mise r test-unit && mise r test-crdsvalidation
  ```
