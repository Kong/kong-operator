# Security Policy

## Reporting a Vulnerability

At Kong, we take security issues very seriously. If you believe you have found a security vulnerability in our project, we encourage you to disclose it responsibly. Please report any potential security vulnerabilities to us by sending an email to [vulnerability@konghq.com](mailto:vulnerability@konghq.com).

## How to Report

1. **Do not publicly disclose the vulnerability**: Please do not create a GitHub issue or post the vulnerability on public forums. Instead, contact us directly at [vulnerability@konghq.com](mailto:vulnerability@konghq.com).
1. **Provide detailed information**: When reporting a vulnerability, please include as much information as possible to help us understand and reproduce the issue. This may include:
   - Description of the vulnerability
   - Steps to reproduce the issue
   - Potential impact
   - Any relevant logs or screenshots

## What to Expect

- **Acknowledgment**: We will acknowledge receipt of your vulnerability report within 48 hours.
- **Investigation**: Our security team will investigate the report and will keep you informed of the progress. We aim to resolve critical vulnerabilities within 30 days of confirmation.
- **Disclosure**: We prefer coordinated disclosure and will work with you to schedule the disclosure of the vulnerability in a way that minimizes the risk to users.

## Bug Bounty Program

We encourage security researchers to participate in our bug bounty program as outlined on the [Kong Vulnerability Disclosure](https://konghq.com/compliance/bug-bounty) page. This program provides rewards for discovering and reporting security vulnerabilities in accordance with our disclosure guidelines.

Thank you for helping to keep Kong secure.

For more information on our security policies and guidelines, please visit the [Kong Vulnerability Disclosure](https://konghq.com/compliance/bug-bounty) page.

## Verifying Released Images and SBOMs

Kong Operator images are built, signed and attested by GitHub Actions with GitHub's OIDC identity, so
a released image can be traced back to this repository, its commit and the workflow that produced it.

Every release attaches to its GitHub Release page:

- a **source SBOM** in SPDX (`source-sbom.spdx.json`) and CycloneDX (`source-sbom.cyclonedx.json`)
- an **image SBOM** per platform: `image-linux-amd64-sbom.*.json` and `image-linux-arm64-sbom.*.json`
- a `SHA256SUMS` covering all of the above

The same reports are also attached to the image as attestations (see *Verify the SBOM, vulnerability
and CIS attestations*), so they do not depend on the Release assets staying around.

The signature and the build provenance describe the **multi-arch index digest**, never a
per-platform digest and never a tag. Collect it first ([`regctl`](https://github.com/regclient/regclient/blob/main/docs/install.md)
is one way):

```sh
IMAGE=kong/kong-operator
TAG=2.4.0
IMAGE_DIGEST=$(regctl manifest digest "${IMAGE}:${TAG}")
```

### Verify the cosign signature

The signature is an OCI 1.1 referrer of the image, attached in the image's own repository, so cosign
finds it with no extra configuration:

```sh
cosign verify \
   "${IMAGE}:${TAG}@${IMAGE_DIGEST}" \
   --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
   --certificate-identity-regexp='^https://github\.com/Kong/kong-operator/\.github/workflows/'
```

The GitHub owner is case-sensitive (`Kong/kong-operator`, not `kong/kong-operator`).

### Verify the build provenance

With [`slsa-verifier`](https://github.com/slsa-framework/slsa-verifier#installation):

```sh
slsa-verifier verify-image \
   "${IMAGE}:${TAG}@${IMAGE_DIGEST}" \
   --print-provenance \
   --source-uri 'github.com/Kong/kong-operator'
```

Or with cosign, checking the identity of the generator that produced the attestation:

```sh
cosign verify-attestation \
   "${IMAGE}:${TAG}@${IMAGE_DIGEST}" \
   --type='slsaprovenance' \
   --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
   --certificate-identity-regexp='^https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@refs/tags/v[0-9]+.[0-9]+.[0-9]+$'
```

### Verify the SBOM, vulnerability and CIS attestations

The reports behind the Release assets are also published as attestations on the image itself, one per
predicate type. `cosign tree` lists what is attached; the types are:

| Predicate type | Report |
|---|---|
| `cyclonedx`, `spdxjson` | source SBOM |
| `https://cyclonedx.org/bom/image/<arch>`, `https://spdx.dev/Document/image/<arch>` | image SBOM, per platform |
| `https://cosign.sigstore.dev/sarif/vuln/source`, `https://cosign.sigstore.dev/sarif/vuln/image/<arch>` | Grype vulnerability report |
| `https://cisecurity.org/docker/<arch>` | CIS Docker benchmark report |
| `https://konghq.com/build-metadata` | workflow and runner context of the build |

```sh
cosign verify-attestation \
   "${IMAGE}:${TAG}@${IMAGE_DIGEST}" \
   --type='cyclonedx' \
   --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
   --certificate-identity-regexp='^https://github\.com/Kong/kong-operator/\.github/workflows/'
```

### Verify the SBOM files

Download the SBOMs and `SHA256SUMS` from the Release page, then check them:

```sh
sha256sum -c SHA256SUMS
```

`SHA256SUMS` proves the files were not corrupted in transit; it is not signed, so it does not prove who
produced them. Authorship is carried by the image: the signature and the provenance above cover the
index digest, and each image SBOM names the platform digest it describes.

## Contact

For any questions or further assistance, please contact us at [vulnerability@konghq.com](mailto:vulnerability@konghq.com).
