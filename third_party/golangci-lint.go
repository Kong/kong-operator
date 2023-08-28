//go:build third_party

package third_party

import (
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
)

//go:generate go install github.com/golangci/golangci-lint/cmd/golangci-lint
