## TODO

### Logic to override Type names

Generator can override type names.
When it sees that a POST operation has the `x-speakeasy-entity-operation` extension with a `crd-name` key, it will use the specified name for the generated CRD type.

### TODOs

- Generated conversion functions unit tests have to check the actual conversion logic, not just the presence of the functions and that not error has been returned.
