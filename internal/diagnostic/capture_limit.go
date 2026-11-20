package diagnostic

import "context"

const MaxCaptureLimitBytes = int64(maxStreamBytes)

type captureLimitContextKey struct{}

func withCaptureLimit(ctx context.Context, limit int64) context.Context {
	return context.WithValue(ctx, captureLimitContextKey{}, limit)
}

func captureLimit(ctx context.Context) int64 {
	if limit, ok := ctx.Value(captureLimitContextKey{}).(int64); ok && limit > 0 && limit <= MaxCaptureLimitBytes {
		return limit
	}
	return MaxCaptureLimitBytes
}
