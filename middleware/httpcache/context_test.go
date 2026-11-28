package httpcache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type ctxKey struct{}

func TestDetach(t *testing.T) {
	tests := []struct {
		run  func(t *testing.T, values context.Context, cancelValues context.CancelFunc)
		name string
	}{
		{
			name: "keeps values and ignores request cancellation",
			run: func(t *testing.T, values context.Context, cancelValues context.CancelFunc) {
				t.Helper()

				ctx, cancel, _ := detach(values, context.Background())
				defer cancel()

				cancelValues()

				assert.Equal(t, "v", ctx.Value(ctxKey{}))
				assert.NoError(t, ctx.Err())
			},
		},
		{
			name: "takes deadline from lifetime context",
			run: func(t *testing.T, values context.Context, _ context.CancelFunc) {
				t.Helper()

				lifetime, cancelLifetime := context.WithTimeout(context.Background(), time.Hour)
				defer cancelLifetime()

				ctx, cancel, _ := detach(values, lifetime)
				defer cancel()

				want, _ := lifetime.Deadline()
				got, ok := ctx.Deadline()

				assert.True(t, ok)
				assert.Equal(t, want, got)
			},
		},
		{
			name: "cancelled with lifetime context",
			run: func(t *testing.T, values context.Context, _ context.CancelFunc) {
				t.Helper()

				lifetime, cancelLifetime := context.WithCancel(context.Background())

				ctx, cancel, _ := detach(values, lifetime)
				defer cancel()

				cancelLifetime()

				assert.Eventually(t, func() bool { return ctx.Err() != nil }, time.Second, time.Millisecond)
				assert.ErrorIs(t, context.Cause(ctx), context.Canceled)
			},
		},
		{
			name: "unlinked context outlives lifetime context but keeps its deadline",
			run: func(t *testing.T, values context.Context, _ context.CancelFunc) {
				t.Helper()

				lifetime, cancelLifetime := context.WithTimeout(context.Background(), time.Hour)

				ctx, cancel, unlink := detach(values, lifetime)
				defer cancel()

				unlink()
				cancelLifetime()

				want, _ := lifetime.Deadline()
				got, ok := ctx.Deadline()

				assert.True(t, ok)
				assert.Equal(t, want, got)
				assert.Never(t, func() bool { return ctx.Err() != nil }, 20*time.Millisecond, time.Millisecond)

				cancel()
				assert.Error(t, ctx.Err())
			},
		},
		{
			name: "cancel func releases context",
			run: func(t *testing.T, values context.Context, _ context.CancelFunc) {
				t.Helper()

				ctx, cancel, _ := detach(values, context.Background())
				cancel()

				assert.Error(t, ctx.Err())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, cancelValues := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "v"))
			defer cancelValues()

			tt.run(t, values, cancelValues)
		})
	}
}
