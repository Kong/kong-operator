//go:build third_party

package third_party

import (
	_ "github.com/go-delve/delve/cmd/dlv"
)

//go:generate go install github.com/go-delve/delve/cmd/dlv
