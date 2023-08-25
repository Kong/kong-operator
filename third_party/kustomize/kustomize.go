//go:build third_party

package kustomize

import (
	_ "sigs.k8s.io/kustomize/kustomize/v5"
)

//go:generate go install sigs.k8s.io/kustomize/kustomize/v5
