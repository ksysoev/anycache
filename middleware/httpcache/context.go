package httpcache

import "context"

// detach returns a context that carries the values of valuesCtx but takes its
// cancellation and deadline from lifetimeCtx.
//
// The cache runs generators on its own base context, which would drop
// request-scoped values (auth, tracing, loggers). The handler instead gets the
// request values while still honoring the generator timeout and cache shutdown,
// and is not cancelled when the original client disconnects.
func detach(valuesCtx, lifetimeCtx context.Context) (context.Context, context.CancelFunc) {
	ctx := context.WithoutCancel(valuesCtx)

	cancelDeadline := context.CancelFunc(func() {})
	if deadline, ok := lifetimeCtx.Deadline(); ok {
		ctx, cancelDeadline = context.WithDeadline(ctx, deadline)
	}

	ctx, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(lifetimeCtx, func() {
		cancel(context.Cause(lifetimeCtx))
	})

	return ctx, func() {
		stop()
		cancel(context.Canceled)
		cancelDeadline()
	}
}
