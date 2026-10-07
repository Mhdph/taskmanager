package auth

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mhdph/taskmanager/internal/hosted"
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

	return identity, nil
}
