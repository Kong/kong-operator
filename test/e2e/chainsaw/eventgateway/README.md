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

Every observation waits for a JSON object with an explicit `policies` array equal
to the exact expected ID sequence, then checks backend and Kubernetes identities
once. The unready-policy scenario deliberately
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

The drift and extra-policy scenarios wait for the operator's periodic Konnect
sync (60 seconds by default) to notice changes no watched resource reports.
After the chain is deleted, the retained order and identities are checked for
`WINDOW_SECONDS` (default 75, longer than the sync period), every
`CHECK_INTERVAL` seconds (default 5): nothing reconciles a deleted chain, but the
listener and the policies keep reconciling and could still change the order.

Cleanup is registered before resource creation and targets only this namespace's
named probe resources and the exact-name, test-labeled remote gateway. It restores
the probe's authentication if a failure interrupted the no-auth scenario, removes
remote children before their parents, and releases any stranded deletion finalizers
only after verifying that this dedicated remote gateway is absent.

These are management-plane ordering checks, not traffic-routing tests or a
guarantee that GET and PUT are atomic against concurrent out-of-band policy-set
changes. Use a disposable, isolated cluster with this branch's CRDs and operator;
do not replace a shared operator deployment to run this scenario.

## Produce policy chain lifecycle

The `producepolicychain-lifecycle` scenario uses a dedicated test-labeled gateway,
two virtual clusters, two `schemaValidation` parent groups, multiple
`modifyHeaders` children, an `encryptFields` child with a test-owned static key,
and independent roots. It checks full ID sequences independently of creation
order, sibling and whole-group movement, immutable Kubernetes and remote parent
relationships, invalid grouping, missing/unready references, complete-set
mismatches, watch recovery, real policy recreation, periodic drift repair,
independent cluster-level ownership on the same virtual cluster, contender
handover, an empty scope, and deletion without authentication or valid refs.

```bash
make test.e2e.chainsaw \
  CHAINSAW_TEST_DIR=./test/e2e/chainsaw/eventgateway/producepolicychain-lifecycle
```

By default the running operator must use its normal `--konnect-sync-period=60s`.
Only three observations hold state for a full 65-second window: repaired drift,
an unready reference, and an omitted-policy mismatch. Other transitions wait for
current-generation conditions and exact backend convergence, rather than adding
a full sync window to every assertion. The minimum observation budget is 195
seconds; setup, reconciliation, API calls and cleanup add to this. The lifecycle
script has a 15-minute limit and cleanup a separate five-minute limit, leaving
25 minutes of the default 45-minute Event Gateway job budget for setup and other
parallel scenarios. Other suites retain a 30-minute default; the repository's
`GHA_EXTENDED_TIMEOUT_MINUTES` variable can override these job budgets.
This does not change suite-wide controller timing or shorten the Listener test;
total matrix runtime still depends on those other scenarios.

If running an explicitly configured isolated operator with another sync period,
set `KONNECT_SYNC_PERIOD_SECONDS` to its **actual** integer-second value and
`WINDOW_SECONDS` to a strictly larger value. Merely changing the test environment
does not reconfigure the operator. Increase the script timeout when increasing
these windows. These overrides do not change the Listener scenario.

Baselines stay in one process, not cross-step Chainsaw outputs. Each identity
check compares Kubernetes UIDs, current Konnect IDs, policy types, and unchanged
`parent_policy_id`; no Kafka traffic, deeper nesting, Consume ordering, on-prem
support, or atomic GET/PUT guarantee is implied. Authentication restoration runs
on exit before cleanup. Cleanup deletes children before parents, verifies the
exact-name/test-labeled gateway's remote 404 and collection absence, and confirms
the named local resources are absent before returning.

The local helper regressions exercise the actual HTTP helper, condition waiter,
and authentication restoration handler using disposable command stubs:

```bash
go test ./test/e2e/chainsaw/eventgateway/producepolicychain-lifecycle
```
