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

## Listener policy chain lifecycle

The `listenerpolicychain-lifecycle` scenario creates an isolated, test-labeled
Konnect gateway, backend, virtual cluster, and two listeners. It uses
`forward_to_virtual_cluster` policies without sending traffic or deploying Kafka
or a data plane.

It checks reverse creation order and declaration changes, an explicitly empty
scope, out-of-band ordering drift and periodic repair, missing and unprogrammed
references without shortened writes, extra and omitted policies, real policy
deletion/recreation with new Konnect IDs and Kubernetes UIDs, conflicting chains
and handover, and deletion without authentication or valid references. Successful
chains must have every prerequisite condition `True` at the current generation
and no independent `status.id`.

Every observation requires a JSON object with an explicit `policies` array equal
to the exact expected ID sequence, then repeats backend and Kubernetes identity
checks for longer than a full sync period. The unready-policy scenario deliberately
uses another test policy's Konnect name; recovery assigns its own unique name,
without adopting or deleting the original policy.
The no-auth scenario removes only its credential Secret's token key, waits for
current-generation `APIAuthValid=False`, and then deletes the chain with invalid policy references.
It checks retained IDs and UIDs without requiring policies to remain programmed
while their authentication is unavailable.
An exit handler restores the token key and waits for authentication to recover
on success, failure, or handled termination, before ordinary test cleanup runs.

```bash
make test.e2e.chainsaw \
  CHAINSAW_TEST_DIR=./test/e2e/chainsaw/eventgateway/listenerpolicychain-lifecycle
```

The defaults are `KONNECT_SYNC_PERIOD_SECONDS=60`, `WINDOW_SECONDS=75`, and
`CHECK_INTERVAL=5`. Set the first value to the running operator's actual
`--konnect-sync-period` in integer seconds; the script rejects an observation
window that does not exceed it. For an isolated operator configured with
`--konnect-sync-period=5s`, a shorter bounded run is:

```bash
KONNECT_SYNC_PERIOD_SECONDS=5 WINDOW_SECONDS=8 CHECK_INTERVAL=1 \
  make test.e2e.chainsaw \
    CHAINSAW_TEST_DIR=./test/e2e/chainsaw/eventgateway/listenerpolicychain-lifecycle
```

Cleanup is registered before resource creation and targets only this namespace's
named probe resources and the exact-name, test-labeled remote gateway. It restores
the probe's authentication if a failure interrupted the no-auth scenario, removes
remote children before their parents, and releases any stranded deletion finalizers
only after verifying that this dedicated remote gateway is absent.

These are management-plane ordering checks, not traffic-routing tests or a
guarantee that GET and PUT are atomic against concurrent out-of-band policy-set
changes. Use a disposable, isolated cluster with this branch's CRDs and operator;
do not replace a shared operator deployment to run this scenario.
