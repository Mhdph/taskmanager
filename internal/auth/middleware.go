package auth

import (
	"net/http"
)

type Middleware struct {
	resolver *Resolver

	// devMode skips hosted identity resolution and derives identities
	// from the USER-ID/APP-ID headers (labels/dev builds only).
	devMode bool
}

func NewMiddleware(
	resolver *Resolver,
	devMode bool,
) *Middleware {
	return &Middleware{
		resolver: resolver,
		devMode:  devMode,
	}
}

func (m *Middleware) Authenticate(
	next http.Handler,
) http.Handler {

	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {

			userID := r.Header.Get("USER-ID")
			appID := r.Header.Get("APP-ID")

			// Dev identity mode: resolve locally from the headers. The
			// hosted platform remains the auth source of truth when
			// dev mode is disabled.
			if m.devMode && userID != "" {
				identity, err := m.resolver.ResolveDev(
					r.Context(),
					ResolveInput{
						UserID: userID,
						AppID:  appID,
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

				return
			}

			token := r.Header.Get("X-APIKEY")
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
