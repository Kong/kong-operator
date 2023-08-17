//go:build third_party

package kustomize

import (
	_ "sigs.k8s.io/kustomize/kustomize/v4"
)

//go:generate go install -modfile go.mod sigs.k8s.io/kustomize/kustomize/v4
