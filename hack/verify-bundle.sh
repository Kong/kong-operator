#!/usr/bin/env bash
# Verifies the generated OLM bundle against the mistakes observed during the
# certification prototyping: operator-sdk generate bundle silently replaced
# spec.relatedImages and dropped webhookdefinitions, and a bundle that carries
# resources OpenShift refuses (Gateway API CRDs, Secrets, NetworkPolicies) or
# CRDs with a conversion webhook section fails the certification.
#
# Usage: hack/verify-bundle.sh [bundle-dir]
set -euo pipefail

BUNDLE_DIR="${1:-bundle}"
CSV="$BUNDLE_DIR/manifests/kong-operator.clusterserviceversion.yaml"
YQ=${YQ:-yq}

fail() {
	echo "verify-bundle: FAIL: $1" >&2
	exit 1
}

[ -f "$CSV" ] || fail "CSV not found at $CSV. Run 'make bundle' first."

# The operator image must be referenced identically everywhere in the CSV.
# make bundle appends the relatedImages operator entry from OPERATOR_IMAGE; a
# later write used to put the base placeholder back, which left two different
# operator images in one bundle.
operator_image="$("$YQ" -r '([.spec.relatedImages[] | select(.name == "operator")][0].image) // ""' "$CSV")"
[ -n "$operator_image" ] ||
	fail "spec.relatedImages has no operator entry; make bundle appends it from OPERATOR_IMAGE"
container_image="$("$YQ" -r '.metadata.annotations.containerImage' "$CSV")"
[ "$container_image" = "$operator_image" ] ||
	fail "annotations.containerImage ($container_image) does not match the relatedImages operator image ($operator_image)"
deploy_images="$("$YQ" -r '.spec.install.spec.deployments[].spec.template.spec.containers[].image' "$CSV" | sort -u)"
[ "$deploy_images" = "$operator_image" ] ||
	fail "install strategy container images ($(echo "$deploy_images" | tr '\n' ' ')) do not match the relatedImages operator image ($operator_image)"

# Related images other than the operator are pinned on every build. Only the
# operator image may be a tag, and only for PR and local builds.
other_images="$("$YQ" -r '.spec.relatedImages[] | select(.name != "operator") | .image' "$CSV")"
unpinned="$([ -n "$other_images" ] && echo "$other_images" | grep -Ev '@sha256:[0-9a-f]{64}$' || true)"
[ -z "$unpinned" ] ||
	fail "these relatedImages entries are not pinned by digest: $(echo "$unpinned" | tr '\n' ' ')"

if [ "${ALLOW_UNPINNED_IMAGES:-false}" = "true" ]; then
	echo "verify-bundle: WARNING: the operator image is not pinned by digest: $operator_image" >&2
else
	echo "$operator_image" | grep -Eq '@sha256:[0-9a-f]{64}$' ||
		fail "the operator image must be pinned by digest: $operator_image (set ALLOW_UNPINNED_IMAGES=true for PR and local builds)"
	for img in $("$YQ" -r '.. | select(type == "!!str") | select(test("^docker://|^registry|^quay.io|^ghcr.io|^docker.io")) | select(test("@sha256:") | not)' "$CSV" 2>/dev/null); do
		[ -z "$img" ] && continue
		fail "unpinned image reference found in the bundle: $img"
	done
fi

# Both webhook definitions must be present: the conversion webhook with the
# three conversion CRDs, and the validating webhook.
[ "$("$YQ" '.spec.webhookdefinitions | length' "$CSV")" = "2" ] ||
	fail "expected 2 webhookdefinitions; operator-sdk drops them silently"
[ "$("$YQ" '[.spec.webhookdefinitions[] | select(.type == "ConversionWebhook")][0].conversionCRDs | length' "$CSV")" = "3" ] ||
	fail "the conversion webhookdefinition must list the 3 conversion CRDs"

# No CRD in the bundle may carry a conversion webhook section: OLM adds it
# from the CSV webhookdefinitions.
conv_crds="$(grep -l "conversion:" "$BUNDLE_DIR"/manifests/*.yaml 2>/dev/null || true)"
for f in $conv_crds; do
	fail "$f carries a spec.conversion section; OLM derives it from webhookdefinitions"
done

# The bundle strips schema descriptions to stay under the size limit. The
# stripper must not remove a schema property that is itself named description,
# which would drop a real field from the bundled CRD: compare with config/crd.
SRC_CRD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/config/crd/kong-operator"
properties_query='[.. | select(tag == "!!map") | select(has("properties")) | select(.properties | type == "!!map") | .properties | keys[]] | unique[]'
if [ -d "$SRC_CRD_DIR" ]; then
	for f in "$BUNDLE_DIR"/manifests/*.yaml; do
		[ "$("$YQ" '.kind' "$f" 2>/dev/null || true)" = "CustomResourceDefinition" ] || continue
		src="$SRC_CRD_DIR/$(basename "$f")"
		[ -f "$src" ] || continue
		lost="$(comm -23 \
			<(("$YQ" -r "$properties_query" "$src") | sort) \
			<(("$YQ" -r "$properties_query" "$f") | sort) | tr '\n' ' ')"
		[ -z "$lost" ] || fail "$(basename "$f") lost schema properties in the bundle: $lost"
	done
fi

# OpenShift ships its own Gateway API CRDs and refuses bundles that
# carry them; Secrets and NetworkPolicies also fail Red Hat's checks.
for f in "$BUNDLE_DIR"/manifests/*gateway.networking.k8s.io*.yaml; do
	[ -e "$f" ] && fail "the bundle carries Gateway API CRDs"
done
for f in "$BUNDLE_DIR"/manifests/*.yaml; do
	kind="$("$YQ" '.kind' "$f" 2>/dev/null || true)"
	case "$kind" in
	Secret | NetworkPolicy) fail "$f has kind $kind, which fails Red Hat's checks" ;;
	esac
done

# The OpenShift version range annotation must be present, with at least one
# supported version.
grep -q "com.redhat.openshift.versions" "$BUNDLE_DIR/metadata/annotations.yaml" ||
	fail "com.redhat.openshift.versions missing from bundle/metadata/annotations.yaml"

# All seven feature annotations must be present.
for f in disconnected fips-compliant ha iam-provider proxy-aware single-node tls-capable; do
	"$YQ" -e ".metadata.annotations[\"features.operators.openshift.io/$f\"]" "$CSV" >/dev/null ||
		fail "features.operators.openshift.io/$f annotation missing"
done

echo "verify-bundle: OK"
