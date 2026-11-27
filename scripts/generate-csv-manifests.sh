#!/usr/bin/env bash
# Generates the ClusterServiceVersion fragments that operator-sdk does not:
# the owned CRD list (spec.customresourcedefinitions.owned) and the
# alm-examples annotation. Both are written in place into the CSV base.
#
# operator-sdk generate bundle copies the base CSV as-is, so these must be
# present before bundle generation. Missing descriptions or examples trip
# Red Hat's good-practices checks.
#
# Usage: scripts/generate-csv-manifests.sh
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CRD_DIR="$PROJECT_DIR/config/crd/kong-operator"
SAMPLES_DIR="$PROJECT_DIR/config/samples"
CSV="$PROJECT_DIR/config/manifests/bases/kong-operator.clusterserviceversion.yaml"

YQ=${YQ:-yq}

if ! command -v "$YQ" >/dev/null 2>&1; then
	echo "yq (mikefarah v4) is required. Install it or run 'make tools'." >&2
	exit 1
fi

# Owned CRD entries: name, kind, storage version, kind as display name, and
# the first line of the CRD root description.
owned=""
empty_descriptions=0
# C collation: the entry order must not depend on the locale, or the committed
# CSV base differs between macOS and the Linux CI runners.
while IFS= read -r crd; do
	[ "$(basename "$crd")" = "kustomization.yaml" ] && continue
	obj="$("$YQ" -o json -I=0 '
		{
			"kind": .spec.names.kind,
			"name": .metadata.name,
			"version": ([.spec.versions[] | select(.storage == true) | .name][0]),
			"displayName": .spec.names.kind,
			"description": ((.spec.versions[0].schema.openAPIV3Schema.description // "") | split("\n")[0])
		}' "$crd")"
	owned="${owned:+$owned,}$obj"
	if [ "$("$YQ" -r '.description | length' <<< "$obj")" -eq 0 ]; then
		empty_descriptions=$((empty_descriptions + 1))
	fi
done < <(printf '%s\n' "$CRD_DIR"/*.yaml | LC_ALL=C sort)
if [ "$empty_descriptions" -gt 0 ]; then
	echo "WARNING: $empty_descriptions owned CRDs have an empty description." >&2
fi
owned_json="[$owned]"

# One example per owned kind, taken from the first top-level sample that
# declares it. Feature-gated samples are excluded.
examples=""
missing_kinds=""
while IFS= read -r kind; do
	export kind
	sample=""
	# Top-level samples only: subdirectories hold samples that need extra
	# setup and are skipped by make test.samples, so they must not become
	# alm-examples either.
	# Several kinds appear in more than one sample. Sort the candidates with C
	# collation so the chosen one does not depend on the locale or the
	# directory order: en_US and C order "-" and "_" differently.
	while IFS= read -r candidate; do
		if grep -q "^kind: $kind\$" "$candidate" 2>/dev/null; then
			sample="$candidate"
			break
		fi
	done < <(printf '%s\n' "$SAMPLES_DIR"/*.yaml | LC_ALL=C sort)
	if [ -z "$sample" ]; then
		missing_kinds="${missing_kinds:+$missing_kinds, }$kind"
		continue
	fi
	# First match only: some sample files declare the same kind more than
	# once. yq ea has to emit the whole array before indexing it (indexing
	# inline makes yq evaluate per document).
	obj="$("$YQ" ea -o json -I=0 "[select(.kind == strenv(kind))]" "$sample" | "$YQ" -o json -I=0 '.[0]')"
	[ "$obj" = "null" ] && { echo "WARNING: sample for CRD kind $kind could not be extracted." >&2; continue; }
	examples="${examples:+$examples,}$obj"
done < <("$YQ" -r '.[].kind' <<< "$owned_json" | LC_ALL=C sort -u)
if [ -n "$missing_kinds" ]; then
	echo "WARNING: no sample found for CRD kinds: $missing_kinds" >&2
fi

# Two separate invocations: chaining two assignments with a pipe makes the
# first one drop in yq. The alm-examples annotation must stay a JSON-encoded
# string, not a YAML list, hence plain strenv.
EXAMPLES="[$examples]" "$YQ" -i '.metadata.annotations.alm-examples = strenv(EXAMPLES)' "$CSV"
OWNED="$owned_json" \
	"$YQ" -i '.spec.customresourcedefinitions.owned = (strenv(OWNED) | fromjson)' "$CSV"

# Re-render once at the end: the assignment above writes the owned list in
# flow style (one huge line); a final pass normalizes it to block style so
# the CSV base stays reviewable.
"$YQ" -P '.' "$CSV" > "$CSV.tmp" && mv "$CSV.tmp" "$CSV"

echo "CSV base updated: $("$YQ" '.spec.customresourcedefinitions.owned | length' "$CSV") owned CRDs, $("$YQ" -r '.metadata.annotations.alm-examples | fromjson | length' "$CSV") examples."
