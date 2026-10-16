# Input changes for create-only Konnect entities

Konnect does not support updating `EventGatewayStaticKey`,
`AIGatewayDataPlaneCertificate`, or `AIGatewayConsumerCredential` entities.
The operator records a SHA-256 fingerprint of their resolved, user-supplied
create request body in `status.createInputsHash`. This includes values sourced
from Secrets, inline values, and other API spec fields, but excludes
operator-injected Kubernetes metadata and SDK request path parameters.
Secret values themselves are never stored in status.

## Detecting changes

After creation, the operator compares the current resolved inputs with that
fingerprint on reconciliation. Secret changes trigger reconciliation through the
existing Secret watches.

If the inputs change, `CreateInputsApplied` becomes `False` with reason
`ValueChanged`. Its message explains how to apply the change. The entity remains
`Programmed=True` if it is otherwise healthy: it still exists in Konnect, but
Konnect retains its original configuration. The operator does not automatically
delete or replace it.

- For `EventGatewayStaticKey`, create a new object and update the policies to
  reference it. Keep the old key available for data encrypted with it.
- For the two AI Gateway kinds, recreate the Kubernetes object to create a new
  Konnect entity with the desired inputs.

Changing the inputs back to their recorded values clears the drift condition.
Changing only unrelated Secret keys or Kubernetes object metadata does not
report drift. Changing a data source without changing the resulting request
body also does not report drift.

If a referenced Secret or required key is missing after creation,
`SecretRefValid=False` reports the problem without blocking normal reconciliation
of the existing entity. `CreateInputsApplied=Unknown`, with reason
`InputsUnavailable`, indicates that the inputs cannot be compared. The recorded
fingerprint is preserved. Cross-namespace access still requires a valid
`KongReferenceGrant`; revoking it continues to block access and reconciliation.

Successful creation, including the existing static-key recreation path after
out-of-band deletion from Konnect, records the new request's fingerprint and
sets `CreateInputsApplied=True`. This feature does not add automatic
out-of-band deletion recovery to the two AI Gateway kinds.

## Upgrading existing objects

An existing Konnect entity without `status.createInputsHash` uses its current
resolved inputs as the baseline on its first successful comparison.
`CreateInputsApplied=True` with reason `BaselineEstablished` explains that this
is an unverified baseline. Subsequent matches use reason `Unchanged`.

This cannot detect changes made before baselining and does not prove that the
baseline values are applied in Konnect. Konnect does not expose all the original
values needed for that verification. If the inputs are unavailable, baselining
waits until they can be resolved.

The fingerprint is an observation of the creation inputs, not continuous
verification of the remote entity's contents.
