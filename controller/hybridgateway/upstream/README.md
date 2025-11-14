# Upstream Package

This package provides functionality for managing KongUpstream resources in the hybrid gateway controller with proper annotation handling for tracking HTTPRoute references.

## Overview

The upstream package is similar in structure to the target package and provides functions for creating and managing KongUpstream resources. The key feature is that it maintains annotations that track which HTTPRoute resources reference each upstream.

## Key Features

1. **Annotation Management**: Automatically manages the `gateway-operator.konghq.com/hybrid-route` annotation to track which HTTPRoutes reference an upstream
2. **Duplicate Detection**: Prevents duplicate HTTPRoute entries in the annotation
3. **Existing Upstream Handling**: If an upstream already exists in the cluster, it appends the current HTTPRoute to the existing annotation instead of overwriting
4. **Cleanup Support**: Provides functionality to remove HTTPRoute references from annotations when they are no longer needed

## Functions

### UpstreamForRule
Main function that creates or updates a KongUpstream for a given HTTPRoute and rule. This function:
- Generates the upstream name using the existing namegen package
- Checks if an upstream with that name already exists
- If it exists, appends the current HTTPRoute to the existing annotations
- If it doesn't exist, creates a new upstream with proper annotations
- Returns the upstream resource for use by the caller

### appendHTTPRouteToAnnotations  
Internal function that adds an HTTPRoute reference to the hybrid-route annotation if it's not already present. The annotation format is: `namespace/name,namespace2/name2,...`

### RemoveHTTPRouteFromAnnotations
Utility function for cleanup scenarios - removes a specific HTTPRoute reference from the hybrid-route annotation. Useful when HTTPRoutes are deleted or no longer reference the upstream.

## Integration

The package integrates with the existing HTTPRoute converter in `controller/hybridgateway/converter/http_route.go`, replacing the direct upstream creation with a call to `UpstreamForRule()`.

## Testing

The package includes comprehensive tests covering:
- Annotation appending with various scenarios (new, existing, duplicates)  
- Annotation removal with edge cases
- Integration with the existing upstream creation flow

## Usage in http_route.go

The original upstream creation code:
```go
upstream, err := builder.NewKongUpstream().
    WithName(upstreamName).
    WithNamespace(c.route.Namespace).
    WithLabels(c.route, &pRef).
    WithAnnotations(c.route, &pRef).
    WithSpecName(upstreamName).
    WithControlPlaneRef(*cp).Build()
```

Was replaced with:
```go
upstreamPtr, err := upstream.UpstreamForRule(ctx, logger, c.Client, c.route, rule, &pRef, cp)
```

This provides the same functionality but with enhanced annotation management.