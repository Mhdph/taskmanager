package auth

import "context"

type Role string

const (
	RoleAdmin  Role = "Admin"
	RoleMember Role = "Member"
)

type Identity struct {
	UserID   string
	TenantID string

	Email     string
	FirstName string
	LastName  string

	Role Role

	IsSupport bool
}

type contextKey string

const identityContextKey contextKey = "identity"

func WithIdentity(
	ctx context.Context,
	identity Identity,
) context.Context {
	return context.WithValue(
		ctx,
		identityContextKey,
		identity,
	)
}

func IdentityFromContext(
	ctx context.Context,
) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey).(Identity)

	return identity, ok
}
