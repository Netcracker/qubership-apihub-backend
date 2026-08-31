package security

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/responder"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/union"
	log "github.com/sirupsen/logrus"
)

type AuthHandler struct {
	responder *responder.Responder
}

func NewAuthHandler(responder *responder.Responder) *AuthHandler {
	return &AuthHandler{responder: responder}
}

func (a *AuthHandler) Secure(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				a.responder.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		_, user, err := fullAuthStrategy.AuthenticateRequest(r)
		if err != nil {
			if multiError, ok := err.(union.MultiError); ok {
				for _, e := range multiError {
					if customError, ok := e.(*exception.CustomError); ok {
						if customError.Status == http.StatusForbidden {
							a.responder.RespondWithCustomError(w, customError)
							return
						}
					}
				}
			}
			a.respondWithAuthFailedError(w, err)
			return
		}

		r = auth.RequestWithUser(user, r)
		next.ServeHTTP(w, r)
	}
}

func (a *AuthHandler) SecureUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				a.responder.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		user, err := userAuthStrategy.Authenticate(r.Context(), r)
		if err != nil {
			a.respondWithAuthFailedError(w, err)
			return
		}

		r = auth.RequestWithUser(user, r)
		next.ServeHTTP(w, r)
	}
}

func (a *AuthHandler) SecureJWT(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				a.responder.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		user, err := jwtAuthStrategy.Authenticate(r.Context(), r)
		if err != nil {
			a.respondWithAuthFailedError(w, err)
			return
		}

		r = auth.RequestWithUser(user, r)
		next.ServeHTTP(w, r)
	}
}

func (a *AuthHandler) NoSecure(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				a.responder.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		next.ServeHTTP(w, r)
	}
}

func (a *AuthHandler) SecureProxy(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				a.responder.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		//TODO: need to remove customJwtStrategy and use sessionCookie strategy only
		user, err := proxyAuthStrategy.Authenticate(r.Context(), r)
		if err != nil {
			a.respondWithAuthFailedError(w, err)
			return
		}
		r = auth.RequestWithUser(user, r)
		//TODO: remove after frontend testing
		r.Header.Del(CustomJwtAuthHeader)

		cookies := r.Cookies()
		r.Header.Del("Cookie")
		for _, cookieValue := range cookies {
			if cookieValue.Name != AccessTokenCookieName && cookieValue.Name != RefreshTokenCookieName {
				r.AddCookie(cookieValue)
			}
		}
		next.ServeHTTP(w, r)
	}
}

func (a *AuthHandler) RefreshToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				a.responder.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		user, err := refreshTokenStrategy.Authenticate(r.Context(), r)
		if user != nil && user.GetExtensions().Get(SetAccessTokenCookieExt) != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     AccessTokenCookieName,
				Value:    user.GetExtensions().Get(SetAccessTokenCookieExt),
				MaxAge:   int(accessTokenDuration.Seconds()),
				Secure:   productionMode,
				HttpOnly: true,
				Path:     "/",
			})
			w.WriteHeader(http.StatusOK)
		} else {
			if err != nil {
				log.Debugf("Failed to refresh access token: %v", err)
			}
			next.ServeHTTP(w, r)
		}
	}
}

func (a *AuthHandler) SecureMCP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				a.responder.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		user, err := apiKeyStrategy.Authenticate(r.Context(), r)
		if err != nil {
			a.respondWithAuthFailedError(w, err)
			return
		}

		r = auth.RequestWithUser(user, r)
		next.ServeHTTP(w, r)
	})
}

func (a *AuthHandler) respondWithAuthFailedError(w http.ResponseWriter, err error) {
	log.Tracef("Authentication failed: %+v", err)
	customErr := &exception.CustomError{
		Status:  http.StatusUnauthorized,
		Message: http.StatusText(http.StatusUnauthorized),
		Debug:   fmt.Sprintf("%v", err),
	}
	a.responder.RespondWithCustomError(w, customErr)
}
