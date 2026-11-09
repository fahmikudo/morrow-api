package httpx

import "context"

type identityKey struct{}

type identity struct {
	userID    string
	sessionID *string
}

// WithUser stores the authenticated identity on the request context.
// `sessionID` is nil for personal access tokens, which have no session row.
func WithUser(ctx context.Context, userID string, sessionID *string) context.Context {
	return context.WithValue(ctx, identityKey{}, identity{userID: userID, sessionID: sessionID})
}

// UserID returns the authenticated user id, or "" when unauthenticated.
func UserID(ctx context.Context) string {
	current, ok := ctx.Value(identityKey{}).(identity)
	if !ok {
		return ""
	}
	return current.userID
}

// SessionID returns the session id and whether the request carried one.
func SessionID(ctx context.Context) (*string, bool) {
	current, ok := ctx.Value(identityKey{}).(identity)
	if !ok {
		return nil, false
	}
	return current.sessionID, true
}

type logIdentityKey struct{}

// logIdentity lets the request logger read the user resolved *inside* the
// auth middleware, after the handler has run.
type logIdentity struct {
	userID string
}
