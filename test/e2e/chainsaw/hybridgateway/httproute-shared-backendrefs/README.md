# HTTPRoute Shared BackendRefs Test

This test validates the Hybrid Gateway controller behavior when multiple `HTTPRoute`s share the same set of `backendRefs`.

It covers the following scenarios:

- Create 3 `HTTPRoute`s using the same `backendRefs` set
- Verify only 1 `KongService` is created and its `gateway-operator.konghq.com/hybrid-routes` annotation includes all 3 routes
- Delete 1 `HTTPRoute` and verify the shared `KongService` remains and its `gateway-operator.konghq.com/hybrid-routes` annotation is updated
- Update the remaining `HTTPRoute` to use a different `backendRefs` set and verify a new dedicated `KongService` is created and the old one is updated
- Verify data plane traffic works throughout
