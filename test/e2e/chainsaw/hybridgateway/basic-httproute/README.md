# Basic HTTPRoute Test

This test validates the complete lifecycle of an `HTTPRoute` managed by the Hybrid Gateway controller. It ensures that creating, updating, and routing traffic works as expected.

The test is divided into the following steps:

**Step 1: Create Prerequisite Resources**

- **Goal:** Set up the initial Kubernetes and Kong environment.
- **Actions:**
    - Creates `kong` and `default` namespaces.
    - Deploys two backend applications: `httpbin` and `echo`.
    - Sets up the core Gateway API resources: `GatewayClass`, `GatewayConfiguration`, and a `Gateway` named `kong-foo`.
    - Configures Konnect integration using `KonnectAPIAuthConfiguration`.
- **Verification:** Asserts that all deployments are ready and all gateway-related resources have a `True` status condition, indicating they are correctly programmed and initialized.

**Step 2: Create HTTPRoute**

- **Goal:** Test the creation of a simple `HTTPRoute`.
- **Actions:**
    - Applies an `HTTPRoute` that routes traffic from the `/echo` path to the `echo` service.
- **Verification:** Asserts that the `HTTPRoute` status becomes `Accepted` and all `Programmed` conditions are `True`, confirming that the controller has processed the route.

**Step 3: Verify Generated Kong Resources**

- **Goal:** Ensure the controller translates the `HTTPRoute` into the correct Kong-specific CRDs.
- **Actions:**
    - This step inspects the resources created by the controller in response to the `HTTPRoute`.
- **Verification:**
    - Asserts that a `KongRoute`, `KongService`, `KongUpstream`, and `KongTarget` have been created.
    - Verifies that these resources have the correct labels, annotations, and specifications (e.g., paths, service references) derived from the `HTTPRoute`.
    - Checks that their `status` is `Programmed` and they are correctly linked to the Konnect control plane.

**Step 4: Verify Data Plane Traffic**

- **Goal:** Confirm that the data plane (Kong Gateway proxy) can route traffic according to the `HTTPRoute`.
- **Actions:**
    - Retrieves the external IP address of the gateway's proxy service.
    - Sends HTTP requests to the `/echo` path.
- **Verification:** Asserts that requests sent from both inside and outside the cluster receive a `200 OK` response, confirming end-to-end connectivity.

**Step 5: Update HTTPRoute and Verify Changes**

- **Goal:** Test the controller's ability to handle updates to an existing `HTTPRoute`.
- **Actions:**
    - Applies an updated `HTTPRoute` manifest. The changes include:
        - Switching the backend service from `echo` to `httpbin`.
        - Adding multiple path and HTTP method matches (e.g., `GET /get`, `POST /post`).
        - Disabling `strip_path`.
- **Verification:**
    - Asserts that the old Kong resources associated with the previous `HTTPRoute` version are deleted.
    - Asserts that new Kong resources are created that reflect the updated rules.
    - Performs end-to-end traffic tests for the new endpoints (`/get`, `/post`, etc.) to confirm the data plane configuration was successfully updated.
