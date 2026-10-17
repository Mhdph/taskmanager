package auth

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mhdph/taskmanager/internal/hosted"
	"strings"
)

type ResolveInput struct {
	Token           string
	UserID          string
	AppID           string
	FromSupportMenu string
}

type Resolver struct {
	hostedClient *hosted.Client
	cache        *IdentityCache

	// syncFunc is invoked after every fresh (non-cached) hosted identity
	// resolution. Used to keep the tenant member directory in sync.
	syncFunc func(context.Context, Identity)
}

func NewResolver(
	hostedClient *hosted.Client,
	cache *IdentityCache,
) *Resolver {
	return &Resolver{
		hostedClient: hostedClient,
		cache:        cache,
	}
}

// SetSyncFunc registers a callback invoked whenever an identity is resolved
// fresh from hosted (never on cache hits). It must be set before the server
// starts serving requests.
func (r *Resolver) SetSyncFunc(fn func(context.Context, Identity)) {
	r.syncFunc = fn
}

func (r *Resolver) Resolve(
	ctx context.Context,
	input ResolveInput,
) (Identity, error) {

	key := buildCacheKey(input)

	if identity, ok := r.cache.Get(key); ok {
		return identity, nil
	}

	profile, err := r.hostedClient.NupProfileAppInfo(
		ctx,
		hosted.ProfileRequest{
			Token:           input.Token,
			UserID:          input.UserID,
			AppID:           input.AppID,
			FromSupportMenu: input.FromSupportMenu,
		},
	)
	if err != nil {
		return Identity{}, fmt.Errorf(
			"resolve hosted identity: %w",
			err,
		)
	}

	if profile.User.ID != input.UserID {
		return Identity{}, fmt.Errorf("user identity mismatch")
	}

	if profile.App.ID != input.AppID {
		return Identity{}, fmt.Errorf("tenant identity mismatch")
	}

	isSupport := false

	if input.FromSupportMenu != "" {
		isSupport, err = strconv.ParseBool(input.FromSupportMenu)
		if err != nil {
			return Identity{}, fmt.Errorf(
				"invalid support mode: %w",
				err,
			)
		}
	}

	role := Role(profile.ACL.Name)

	switch role {
	case RoleAdmin, RoleMember:
	default:
		return Identity{}, fmt.Errorf(
			"unsupported app role: %s",
			profile.ACL.Name,
		)
	}

	identity := Identity{
		UserID:    profile.User.ID,
		TenantID:  profile.App.ID,
		Email:     profile.User.Email,
		FirstName: profile.User.FirstName,
		LastName:  profile.User.LastName,
		Role:      role,
		IsSupport: isSupport,
	}

	r.cache.Set(key, identity)

	if r.syncFunc != nil {
		r.syncFunc(ctx, identity)
	}

	return identity, nil
}

// ResolveDev derives an identity without contacting the hosted platform:
// USER-ID headers become the user, APP-ID the tenant. Admin + support so
// local development can reach every feature. The directory sync callback
// still runs, so the workspace member directory is populated.
func (r *Resolver) ResolveDev(
	ctx context.Context,
	input ResolveInput,
) (Identity, error) {

	if input.UserID == "" {
		return Identity{}, fmt.Errorf(
			"dev identity requires a USER-ID header",
		)
	}

	tenantID := input.AppID

	if tenantID == "" {
		tenantID = "dev-tenant"
	}

	identity := Identity{
		UserID:    input.UserID,
		TenantID:  tenantID,
		Email:     input.UserID + "@dev.local",
		FirstName: strings.TrimSpace(input.UserID),
		Role:      RoleAdmin,
		IsSupport: true,
	}

	r.cache.Set(buildCacheKey(input), identity)

	if r.syncFunc != nil {
		r.syncFunc(ctx, identity)
	}

	return identity, nil
}
