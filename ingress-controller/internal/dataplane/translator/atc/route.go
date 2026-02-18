package atc

import (
	"github.com/kong/go-kong/kong"
)

// ApplyExpression sets a Matcher as a Kong route's expression and assigns the route the given priority.
func ApplyExpression(r *kong.Route, m Matcher, priority uint64) {
	r.Expression = new(m.Expression())
	r.Priority = new(priority)
}
