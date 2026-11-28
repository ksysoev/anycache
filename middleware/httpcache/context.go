package httpcache

import "context"

// detach returns a context that carries the values of valuesCtx but takes its
// cancellation and deadline from lifetimeCtx.
//
// The cache runs generators on its own base context, which would drop
// request-scoped values (auth, tracing, loggers). The handler instead gets the
// request values while still honoring the generator timeout and cache shutdown,
// and is not cancelled when the original client disconnects.
//
// The returned cancel func releases the context. The returned unlink func stops
// propagating the cancellation of lifetimeCtx while keeping its deadline, so the
// context can outlive lifetimeCtx (a response streamed after the generator
// returned); the context must then still be released with cancel.
func detach(valuesCtx, lifetimeCtx context.Context) (ctx context.Context, cancel, unlink context.CancelFunc) {
	ctx = context.WithoutCancel(valuesCtx)

	cancelDeadline := context.CancelFunc(func() {})
	if deadline, ok := lifetimeCtx.Deadline(); ok {
		ctx, cancelDeadline = context.WithDeadline(ctx, deadline)
	}

	ctx, cancelCause := context.WithCancelCause(ctx)
	stop := context.AfterFunc(lifetimeCtx, func() {
		cancelCause(context.Cause(lifetimeCtx))
	})

	cancel = func() {
		stop()
		cancelCause(context.Canceled)
		cancelDeadline()
	}

	unlink = func() { stop() }

	return ctx, cancel, unlink
}
