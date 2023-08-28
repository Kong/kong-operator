//go:build third_party

package skaffold

import (
	_ "github.com/GoogleContainerTools/skaffold/v2/cmd/skaffold"
)

//go:generate go install github.com/GoogleContainerTools/skaffold/v2/cmd/skaffold
