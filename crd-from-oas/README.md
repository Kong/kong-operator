## crd-from-oas

This is a tool to generate Kubernetes CRD definitions and conversion functions
from OpenAPI Specification (OAS) files.
It is designed to help create CRDs that are consistent with API specifications,
reducing the manual effort required to maintain CRD definitions and ensuring that
they stay up-to-date with the API changes.

### TODOs

- Generated conversion functions unit tests have to check the actual conversion logic, not just the presence of the functions and that not error has been returned.

- Research feasibility of generating CRD validation tests from OAS spec. If feasible, implement it. If not, add scaoffolding generation which will require users to fill in the test cases manually.
