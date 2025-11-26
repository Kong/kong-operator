# Chainsaw HybridGateway Test Guide

This README provides instructions for running Chainsaw tests in the `hybridgateway` directory.

## 1. Install Chainsaw

Follow the official Chainsaw installation guide:
- [Chainsaw Documentation](https://kyverno.github.io/chainsaw/latest/quick-start/install/)

## 2. Required Environment Variables


Before running the tests, export the following environment variables:

```
export KUBECONFIG=/path/to/your/kubeconfig
export KONNECT_TOKEN=your-konnect-api-token
export KONNECT_SERVER_URL=eu.api.konghq.tech
```

Adjust the values as needed for your environment. The `KONNECT_TOKEN` and `KONNECT_SERVER_URL` are required for Konnect API authentication and are referenced in the test manifests.

## 3. Run the Tests

Use the following command to execute the Chainsaw tests:


```
chainsaw test --test-dir test/e2e/chainsaw/hybridgateway/basic-httproute
```

- `--test-dir`: Path to the test directory
- `--skip-delete`: Prevents deletion of resources after tests

For more options, see the [Chainsaw CLI documentation](https://kyverno.github.io/chainsaw/latest/).
