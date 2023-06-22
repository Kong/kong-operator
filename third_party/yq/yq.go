//go:build third_party

package yq

import (
	_ "github.com/mikefarah/yq/v4"
)

//go:generate go install -modfile go.mod github.com/mikefarah/yq/v4
