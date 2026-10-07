package auth

import (
	"net/http"
)

type Middleware struct {
	resolver *Resolver
}

func NewMiddleware(resolver *Resolver) *Middleware {
	return &Middleware{
		resolver: resolver,
	}
}

func (m *Middleware) Authenticate(
	next http.Handler,
) http.Handler {

	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {

			token := r.Header.Get("X-APIKEY")
			userID := r.Header.Get("USER-ID")
			appID := r.Header.Get("APP-ID")
			fromSupportMenu := r.Header.Get(
				"FROM-SUPPORT-MENU",
			)

			if token == "" ||
				userID == "" ||
				appID == "" {

				http.Error(
					w,
					"missing authentication headers",
					http.StatusUnauthorized,
				)
				return
			}

			identity, err := m.resolver.Resolve(
				r.Context(),
				ResolveInput{
					Token:           token,
					UserID:          userID,
					AppID:           appID,
					FromSupportMenu: fromSupportMenu,
				},
			)

			if err != nil {
				http.Error(
					w,
					"unauthorized",
					http.StatusUnauthorized,
				)
				return
			}

			ctx := WithIdentity(
				r.Context(),
				identity,
			)

			next.ServeHTTP(
				w,
				r.WithContext(ctx),
			)
		},
	)
}
