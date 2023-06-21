//go:build third_party

package third_party

import (
	_ "github.com/operator-framework/operator-registry/cmd/opm"
)

//go:generate go install -modfile go.mod github.com/operator-framework/operator-registry/cmd/opm
