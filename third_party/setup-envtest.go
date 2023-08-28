//go:build third_party

package third_party

import (
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"
)

//go:generate go install sigs.k8s.io/controller-runtime/tools/setup-envtest
