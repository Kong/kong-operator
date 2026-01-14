# Cleanup Script

This script cleans up orphaned resources created by CI tests.

## Konnect Control Planes Cleanup

The script will delete Konnect Control Planes that meet **all** of the following conditions:

1. Has one of these labels:
   - `operator-test-id` (used by integration tests)
   - `k8s-kind:KonnectGatewayControlPlane` (automatically added by Kong Operator)
2. Was created more than 1 hour ago

**Warning**: All Control Planes managed by Kong Operator (with label `k8s-kind:KonnectGatewayControlPlane`) in the configured Konnect organization will be pruned by this script.

## Usage

```bash
go run ./hack/cleanup [mode]
```

Where `mode` is one of:
- `all` (default): clean up both GKE clusters and Konnect control planes
- `gke`: clean up only GKE clusters
- `konnect`: clean up only Konnect control planes

## Environment Variables

- `KONG_TEST_KONNECT_ACCESS_TOKEN`: Konnect API access token
- `KONG_TEST_KONNECT_SERVER_URL`: Konnect API server URL
