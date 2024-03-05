package ctxinjector

import (
	"context"
)

// NewCtxInjector creates a new context injector with the given injectors.
func NewCtxInjector(injectors ...KeyValueInjectorFunc) CtxInjector {
	return CtxInjector{
		injectors: injectors,
	}
}

// CtxInjector is a context injector that injects key-value pairs into a context.
// When list of injector is not defined, it does nothing.
type CtxInjector struct {
	injectors []KeyValueInjectorFunc
}

// Register adds a new injector to the context injector.
func (ci *CtxInjector) Register(injector ...KeyValueInjectorFunc) {
	ci.injectors = append(ci.injectors, injector...)
}

// KeyValueInjectorFunc is type of a function that returns a key-value pair
// to be injected into a context.
type KeyValueInjectorFunc func() (key any, value any)

// InjectKeyValues injects key-value pairs into a context and returns the new context.
// It iterates over the injectors and calls each one to get a key-value pair to inject.
func (ci *CtxInjector) InjectKeyValues(ctx context.Context) context.Context {
	for _, injector := range ci.injectors {
		key, value := injector()
		ctx = context.WithValue(ctx, key, value)
	}
	return ctx
}
