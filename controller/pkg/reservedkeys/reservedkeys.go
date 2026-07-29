package reservedkeys

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// IsReservedFunc reports whether a label/annotation key is reserved for
// internal operator or Kubernetes use and must be dropped from any
// user-supplied labels/annotations.
type IsReservedFunc func(key string) bool

// NewChecker returns an IsReservedFunc that reserves any key carrying the
// operator's common label prefix (consts.OperatorLabelPrefix), plus any of
// the exact keys passed in extraExact (e.g. a hardcoded selector label such
// as "app", or Kubernetes-managed keys such as
// "deployment.kubernetes.io/revision").
func NewChecker(extraExact ...string) IsReservedFunc {
	exact := make(map[string]struct{}, len(extraExact))
	for _, k := range extraExact {
		exact[k] = struct{}{}
	}
	return func(key string) bool {
		if strings.HasPrefix(key, consts.OperatorLabelPrefix) {
			return true
		}
		_, ok := exact[key]
		return ok
	}
}

// MetadataType identifies whether a set of keys being filtered are labels or
// annotations, for logging purposes.
type MetadataType string

func (m MetadataType) String() string {
	return string(m)
}

const (
	// MetadataTypeLabel identifies a map of Kubernetes labels.
	MetadataTypeLabel MetadataType = "label"
	// MetadataTypeAnnotation identifies a map of Kubernetes annotations.
	MetadataTypeAnnotation MetadataType = "annotation"
)

// Filter drops any key from keys for which isReserved returns true, logging
// an Info message for each one dropped so it's clear why a user-provided
// label/annotation didn't take effect. extraLogFields are appended verbatim
// to the log call (e.g. "dataplane", "ns/name") to identify the owning
// resource.
//
// This is logged at Info rather than Error level: it's an expected, routine
// situation (not a bug), and controller-runtime's zap logger attaches a full
// stack trace to every Error-level log line in production mode, which would
// otherwise spam the logs on every reconcile of an object whose spec sets
// a reserved key.
func Filter(
	logger logr.Logger, metadataType MetadataType, obj metav1.Object, isReserved IsReservedFunc,
) map[string]string {
	var keys map[string]string
	switch metadataType {
	case MetadataTypeLabel:
		keys = obj.GetLabels()
	case MetadataTypeAnnotation:
		keys = obj.GetAnnotations()
	default:
		panic(fmt.Sprintf("unknown metadataType %q", metadataType))
	}
	if len(keys) == 0 {
		return nil
	}

	filtered := make(map[string]string, len(keys))
	for k, v := range keys {
		if isReserved(k) {
			log.Info(
				logger,
				"Ignoring reserved key in spec, it is managed by the operator and cannot be overridden",
				"metadataType", metadataType.String(),
				"key", k,
			)
			continue
		}
		filtered[k] = v
	}
	return filtered
}

// Merge returns a new map with base's entries overlaid by additions. It never
// mutates base, so it's safe to call even when base is shared with another
// object (e.g. a Deployment's labels map reused for its Pod template).
func Merge(base, additions map[string]string) map[string]string {
	if len(additions) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(additions))
	maps.Copy(merged, base)
	maps.Copy(merged, additions)
	return merged
}

// MergeAnnotationsTracked behaves like Merge, but also records additions
// (JSON-encoded) under consts.AnnotationLastAppliedAnnotations.
//
// Reconcilers that patch an existing object directly (rather than relying on
// Server-Side Apply to relinquish removed fields automatically) need this so
// that a later call comparing the current spec against this tracked value can
// detect keys that were removed from the spec since the previous reconcile,
// and delete them from the live object instead of leaving them there forever.
func MergeAnnotationsTracked(base, additions map[string]string) map[string]string {
	merged := Merge(base, additions)
	if len(additions) == 0 && merged != nil {
		// Merge returns base by reference when there are no additions; clone
		// it before mutating below so base itself is never mutated, per
		// Merge's contract.
		merged = maps.Clone(merged)
	}
	encoded, err := json.Marshal(additions)
	if err == nil {
		if merged == nil {
			merged = make(map[string]string, 1)
		}
		merged[consts.AnnotationLastAppliedAnnotations] = string(encoded)
	}
	return merged
}

// ExtractOutdated returns the subset of the last-applied annotations encoded
// in existing[consts.AnnotationLastAppliedAnnotations] whose keys are no
// longer present in currentSpec, i.e. annotations that were set by a previous
// reconcile (via MergeAnnotationsTracked) but have since been removed from the
// spec and so must be deleted from the live object.
func ExtractOutdated(currentSpec, existing map[string]string) (map[string]string, error) {
	if existing == nil {
		return nil, nil
	}
	encoded, ok := existing[consts.AnnotationLastAppliedAnnotations]
	if !ok {
		return nil, nil
	}
	outdated := map[string]string{}
	if err := json.Unmarshal([]byte(encoded), &outdated); err != nil {
		return nil, fmt.Errorf("failed to decode last-applied annotations: %w", err)
	}
	for k := range currentSpec {
		delete(outdated, k)
	}
	return outdated, nil
}
