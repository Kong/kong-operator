package sdk

import "context"

type sdkContextKey struct{}

// WithSDK stores the SDK wrapper used for the current reconciliation in the
// context. Write observers use it to read the current token instead of relying
// on a cached SDK instance.
func WithSDK(ctx context.Context, sdk SDKWrapper) context.Context {
	if sdk == nil {
		return ctx
	}
	return context.WithValue(ctx, sdkContextKey{}, sdk)
}

// SDKFromContext returns the SDK wrapper stored by WithSDK.
func SDKFromContext(ctx context.Context) (SDKWrapper, bool) {
	sdk, ok := ctx.Value(sdkContextKey{}).(SDKWrapper)
	return sdk, ok && sdk != nil
}
