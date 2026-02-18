package kongstate

import "github.com/kong/kong-operator/v2/ingress-controller/internal/util"

var _ = util.UUIDGenerator(&StaticUUIDGenerator{})

type StaticUUIDGenerator struct {
	UUID string
}

func (s StaticUUIDGenerator) NewString() string {
	return s.UUID
}
