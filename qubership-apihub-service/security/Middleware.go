package security

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/union"
	log "github.com/sirupsen/logrus"
)

const authTimeout = 15 * time.Second

// authenticate verifies credentials under authTimeout. Routes exempt from RequestTimeoutMiddleware
// (MCP transport, AI chat turns) are exempt for handler processing only
func authenticate(strategy auth.Strategy, r *http.Request) (auth.Info, error) {
	ctx, cancel := context.WithTimeout(r.Context(), authTimeout)
	defer cancel()
	// union.Union.Authenticate ignores its ctx argument and reads r.Context(), so the deadline has
	// to travel on the request as well.
	return strategy.Authenticate(ctx, r.WithContext(ctx))
}

func Secure(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				utils.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		user, err := authenticate(fullAuthStrategy, r)
		if err != nil {
			if multiError, ok := err.(union.MultiError); ok {
				for _, e := range multiError {
					if customError, ok := e.(*exception.CustomError); ok {
						if customError.Status == http.StatusForbidden {
							utils.RespondWithCustomError(w, customError)
							return
						}
					}
				}
			}
			respondWithAuthFailedError(w, r, err)
			return
		}

		r = auth.RequestWithUser(user, r)
		next.ServeHTTP(w, r)
	}
}

func SecureUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				utils.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		user, err := authenticate(userAuthStrategy, r)
		if err != nil {
			respondWithAuthFailedError(w, r, err)
			return
		}

		r = auth.RequestWithUser(user, r)
		next.ServeHTTP(w, r)
	}
}

func SecureJWT(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				utils.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		user, err := authenticate(jwtAuthStrategy, r)
		if err != nil {
			respondWithAuthFailedError(w, r, err)
			return
		}

		r = auth.RequestWithUser(user, r)
		next.ServeHTTP(w, r)
	}
}

func NoSecure(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				utils.RespondWithCustomError(w, &exception.CustomError{
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

func SecureProxy(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				utils.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		//TODO: need to remove customJwtStrategy and use sessionCookie strategy only
		user, err := authenticate(proxyAuthStrategy, r)
		if err != nil {
			respondWithAuthFailedError(w, r, err)
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

func RefreshToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				utils.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		user, err := authenticate(refreshTokenStrategy, r)
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

func SecureMCP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				utils.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		user, err := authenticate(apiKeyStrategy, r)
		if err != nil {
			respondWithAuthFailedError(w, r, err)
			return
		}

		r = auth.RequestWithUser(user, r)
		next.ServeHTTP(w, r)
	})
}

func contextErrorCause(err error) error {
	if multiError, ok := err.(union.MultiError); ok {
		for _, e := range multiError {
			if cause := contextErrorCause(e); cause != nil {
				return cause
			}
		}
		return nil
	}
	if utils.IsRequestTimeout(err) || utils.IsContextCancelled(err) {
		return err
	}
	return nil
}

func respondWithAuthFailedError(w http.ResponseWriter, r *http.Request, err error) {
	if cause := contextErrorCause(err); cause != nil {
		// Nobody is left to read a response
		if utils.IsClientDisconnected(r, cause) {
			log.Infof("Authentication stopped: client closed the request before it completed: %v", err)
			return
		}
		// A request that gave up while authenticating is not an authentication failure, and reporting it
		// as 401 sends the user off to fix credentials that can be perfectly valid.
		log.Errorf("Authentication aborted: %v", err)
		if utils.IsRequestTimeout(cause) {
			utils.RespondWithCustomError(w, &exception.CustomError{
				Status:  http.StatusInternalServerError,
				Code:    exception.RequestTimeout,
				Message: exception.RequestTimeoutMsg,
				Debug:   err.Error(),
			})
			return
		}
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Code:    exception.RequestCancelled,
			Message: exception.RequestCancelledMsg,
			Debug:   err.Error(),
		})
		return
	}
	log.Tracef("Authentication failed: %+v", err)
	customErr := &exception.CustomError{
		Status:  http.StatusUnauthorized,
		Message: http.StatusText(http.StatusUnauthorized),
		Debug:   fmt.Sprintf("%v", err),
	}
	utils.RespondWithJson(w, customErr.Status, customErr)
}
