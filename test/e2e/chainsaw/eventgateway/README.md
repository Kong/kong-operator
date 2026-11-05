# Event Gateway chainsaw e2e

Tests Event Gateway resources against the Konnect API.

## Running locally

Requires a cluster with the current CRDs and the operator's Konnect controllers
enabled, a Konnect token, and `kubectl`, `curl`, `jq`, and Bash.

```bash
KONNECT_TOKEN=<pat> KONNECT_SERVER_URL=<region>.api.konghq.tech \
  make test.e2e.chainsaw CHAINSAW_TEST_DIR=./test/e2e/chainsaw/eventgateway
```

## Virtual cluster policy chain lifecycle

Checks the declared policy order, recovery from remote order drift, and retention
of the order and policies after deleting the chain CR. No Kafka or data plane is
required for this scenario.

With the same environment variables, run only this scenario:

```bash
make test.e2e.chainsaw \
  CHAINSAW_TEST_DIR=./test/e2e/chainsaw/eventgateway/virtualclusterpolicychain-lifecycle
```

The 75-second observation windows assume the default 60-second Konnect sync
period. If the sync period is increased, increase the observation windows and
convergence/exec timeouts accordingly.
