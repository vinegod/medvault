package middleware

import "context"

// ctxKey is a private type so other packages can't accidentally collide
// with our context keys. Each constant of this type is a unique key.
type ctxKey int

const (
	userIDKey ctxKey = iota
	usernameKey
	householdIDKey
	roleKey
)

// UserIDFrom returns the authenticated user's id from ctx, or "" if absent.
func UserIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(userIDKey).(string)
	return v
}

// UsernameFrom returns the authenticated user's username from ctx, or "".
func UsernameFrom(ctx context.Context) string {
	v, _ := ctx.Value(usernameKey).(string)
	return v
}

// HouseholdIDFrom returns the active household id from ctx, or "" if absent.
func HouseholdIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(householdIDKey).(string)
	return v
}

// RoleFrom returns the user's role in the active household, or "".
func RoleFrom(ctx context.Context) string {
	v, _ := ctx.Value(roleKey).(string)
	return v
}
