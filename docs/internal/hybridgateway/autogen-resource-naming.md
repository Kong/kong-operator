# Hybrid Controller auto-generated Kong resources names

## KongRoutes
Are identified by the paths and the serviceRef field (which references a KongService, which gives an indirect reference to the Control Plane).

A KongRoute is created for each HTTPRoute rule. It is uniquely identified by HTTPRoute matches.

Auto-generated name (HR stands for HTTPRoute):

`<HR namespace>-<HR name>.cp<HASH(<Control Plane>).HASH(HR.spec.rule[x].matches)>`

example (HTTPRoute name: default/httproute-echo):

```
default-httproute-echo.cp431ef96.1b69eced
```

## KongService
Are identified by the Control Plane (controlPlaneRef) and the host fields (which will be filled with a KongUpstream name).

A KongService is uniquely identified from a set of BackendRefs of an HTTPRoute. If the same set of BackendRefs appears in different rules of even different HTTPRoutes, only one KongService should be created referenced by multiple KongRoutes.

Auto-generated name:

`cp<HASH(<Conrol Plane>).HASH(HR.spec.rule[x].backendRefs)>`

example:
```
cp431ef96.5a7cc34c
```

## KongUpstream
Same as KongService above.

There is a 1:1 mapping between auto-generated KongServices and KongUpstreams.

Each pair of generated KongService and KongUpstream have an identical name.

Auto-generated name:

`cp<HASH(<Conrol Plane>).HASH(HR.spec.rule[x].backendRefs)>`

example:
```
cp431ef96.5a7cc34c
```

## KongTargets
They can reference one and only one parent KongUpstream.

They are strictly coupled with the KongUpstream.

Each KongTarget is derived by a single Endpoint of a Service referenced by an HTTPRoute BackendRef.

Auto-generated name:

`<KongUpstream.name>.<HASH(HR.spec.rule[x].BackendRef[y],endpoint ip, endpoint port)>`

example:
```
cp431ef96.5a7cc34c.46cb9d28
```

## KongPlugins
They are identified only by their config.

They can be referenced by multiple KongPluginBindings.

They don't need to be coupled with the parentRef (control plane).

Auto-generated name:

`pl<HASH(HR.spec.rule[x].filter[z])>`

example:
```
pl72ae1b9a
```

## KongPluginBindings
They bind a KongRoute with a KongPlugin.

Their natural name is the concatenation of the names of the bound KongRoute and KongPlugin.

Auto-generated name:

`<KongRoute.name>.<KongPlugin.name>`

example:
```
default-httproute-echo.cp431ef96.1b69eced.pl72ae1b9a
```