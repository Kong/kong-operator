# HTTPRoute retries

Gateway API `HTTPRoute.spec.rules[].retry` is supported for connection-error
retries in both standard (KIC-managed) and hybrid Gateway modes. Kong Gateway
retries transport-level failures through the `retries` field of its Service
entity, so this is the only retry dimension the operator can faithfully map.

## Supported configuration

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: retry-example
spec:
  parentRefs:
    - name: example-gateway
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /orders
      retry:
        attempts: 2
      backendRefs:
        - name: orders
          port: 8080
```

`attempts` is the number of retries after the initial attempt:

- `attempts: 0` disables retries.
- Omitting `attempts` inside a `retry` stanza defaults to `1`.
- Omitting the whole `retry` stanza leaves Kong Gateway's Service default
  (`5`) in place.
- The largest accepted value is `32767`, Kong Gateway's Service limit.

When the stanza is configured, Kong Gateway retries transport-level failures
such as a connection that could not be established, a connection refused or
reset before the request was processed, a TLS handshake failure, or a timeout
while reading response headers. Retries are performed by NGINX's next-upstream
mechanism, bounded by the configured attempts and the available upstream
targets.

Kong Gateway does not replay non-idempotent requests once they may already have
been processed by the backend: a `POST` or `PATCH` is retried only when the TCP
connection failed before the upstream received the request. Idempotent methods
may still be retried after a read or write timeout because replaying them is
safe. This preserves the retry-safety requirements of GEP-1731.

## Unsupported configuration

The following fields cannot be honored by Kong Gateway and are rejected through
the Route status with `Accepted=False` and reason `UnsupportedValue`:

- `retry.codes`: Kong Gateway retries transport-level failures only. It does
  not retry HTTP responses such as `500`, `502`, `503`, or `504`, so
  `HTTPRouteRetryCodes` is not advertised.
- `retry.backoff`: Kong Gateway retries immediately and has no configurable
  retry backoff. Any configured `backoff`, including `0s`, is rejected.
- `attempts` greater than `32767`.

Example status for a Route that sets `retry.codes`:

```yaml
status:
  parents:
    - conditions:
        - type: Accepted
          status: "False"
          reason: UnsupportedValue
          message: "rules[0].retry.codes is unsupported: Kong Gateway retries transport-level failures only and cannot retry on HTTP response status codes"
```

## Precedence with konghq.com/retries

The `konghq.com/retries` annotation on a backend Service takes precedence over
`HTTPRoute.spec.rules[].retry.attempts`, matching the precedence used for the
Kong timeout annotations. For example, `attempts: 3` on the Route and
`konghq.com/retries: "7"` on the backend Service result in `7` retries. Because
the annotation wins, it can also override `attempts: 0`.

When the annotation is absent, the Route's retry configuration is used. When
both are absent, Kong Gateway's Service default applies.

Rules that share backend Services but request different retry values are
translated to separate Kong Services, so one rule's retries do not leak into
another rule.

## Timeouts and backoff

An HTTPRoute `timeouts.backendRequest` is mapped to the Kong Service
`connect_timeout`, `read_timeout`, and `write_timeout` settings. These are
per-phase idle timeouts, not a single end-to-end deadline for one attempt or
the combined initial request and retries.

Kong Operator does not advertise `HTTPRouteRequestTimeout`. There is no
operator-managed total request deadline spanning all retry attempts, and Kong
Gateway has no per-route retry backoff. Configure `retry.backoff` only if your
Gateway implementation supports it; this operator rejects it.

## Gateway modes

Both modes use the same Kong Gateway retry semantics:

- **Standard Gateway:** the embedded ingress controller writes the retries
  value to the generated Kong Service.
- **Hybrid Gateway:** the operator writes the retries value to the generated
  `KongService`, which is synchronized to Konnect.

`HTTPRouteRetryConnectionError` is advertised in both modes.
`HTTPRouteRetryCodes` is not advertised in either mode.

## References

- [GEP-1731: HTTPRoute Retries](https://gateway-api.sigs.k8s.io/geps/gep-1731/)
- [Kong Gateway: Proxying with Kong Gateway](https://developer.konghq.com/gateway/traffic-control/proxying/)
