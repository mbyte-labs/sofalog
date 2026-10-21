package httpx

import (
	"context"
	"net/netip"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyClientIP
	ctxKeyUserID
)

// RequestIDFrom returns the request id set by the RequestID middleware, or ""
// outside a request.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}

// ClientIPFrom returns the real client IP resolved once by the RealIP
// middleware. Nothing downstream reads X-Forwarded-For again.
func ClientIPFrom(ctx context.Context) netip.Addr {
	ip, _ := ctx.Value(ctxKeyClientIP).(netip.Addr)
	return ip
}

// WithUserID stamps the authenticated user's id into the context. Set by the
// session middleware — handlers read it back with UserIDFrom and pass it to
// services as an explicit parameter, never the context itself.
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, userID)
}

// UserIDFrom returns the authenticated user's id, or 0 outside an
// authenticated request. 0 is never a valid users.id (ids start at 1), so
// per-user queries scoped to it match nothing rather than someone else's rows.
func UserIDFrom(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxKeyUserID).(int64)
	return id
}
