// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package security

import (
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security/cookie"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/union"
	log "github.com/sirupsen/logrus"
	"net/http"
	"runtime/debug"
	"strconv"
)

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
		_, user, err := fullAuthStrategy.AuthenticateRequest(r)
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
			respondWithAuthFailedError(w, err)
			return
		}
		if user.GetExtensions().Get(SetCookieExt) != "" {
			setSessionCookie(w, user)
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
		user, err := userAuthStrategy.Authenticate(r.Context(), r)
		if err != nil {
			respondWithAuthFailedError(w, err)
			return
		}
		if user.GetExtensions().Get(SetCookieExt) != "" {
			setSessionCookie(w, user)
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
		user, err := jwtAuthStrategy.Authenticate(r.Context(), r)
		if err != nil {
			respondWithAuthFailedError(w, err)
			return
		}
		if user.GetExtensions().Get(SetCookieExt) != "" {
			setSessionCookie(w, user)
		}

		r = auth.RequestWithUser(user, r)
		next.ServeHTTP(w, r)
	}
}

func SecureWebsocket(next http.HandlerFunc) http.HandlerFunc {
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
		//TODO: should be removed
		token := r.URL.Query().Get("token")
		r.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		_, user, err := fullAuthStrategy.AuthenticateRequest(r)
		if err != nil {
			respondWithAuthFailedError(w, err)
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

func SecureAgentProxy(next http.HandlerFunc) http.HandlerFunc {
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
		user, err := proxyAuthStrategy.Authenticate(r.Context(), r)
		if err != nil {
			respondWithAuthFailedError(w, err)
			return
		}
		if user.GetExtensions().Get(SetCookieExt) != "" {
			setSessionCookie(w, user)
		}
		//TODO: add custom header to the request
		r = auth.RequestWithUser(user, r)
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
		user, err := proxyAuthStrategy.Authenticate(r.Context(), r)
		if err != nil {
			respondWithAuthFailedError(w, err)
			return
		}
		if user.GetExtensions().Get(SetCookieExt) != "" {
			setSessionCookie(w, user)
		}
		r = auth.RequestWithUser(user, r)
		//TODO: remove after frontend testing
		r.Header.Del(CustomJwtAuthHeader)
		cookies := r.Cookies()
		r.Header.Del("Cookie")
		for _, cookieValue := range cookies {
			if cookieValue.Name != cookie.SessionCookieName {
				r.AddCookie(cookieValue)
			}
		}
		next.ServeHTTP(w, r)
	}
}

func setSessionCookie(w http.ResponseWriter, user auth.Info) {
	tokenExpTimestamp, _ := strconv.ParseInt(user.GetExtensions().Get(RefreshTokenExpiresAtExt), 0, 64)
	maxAge := utils.GetRemainingSeconds(tokenExpTimestamp)
	http.SetCookie(w, &http.Cookie{
		Name:     cookie.SessionCookieName,
		Value:    user.GetExtensions().Get(SetCookieExt),
		MaxAge:   int(maxAge),
		Secure:   true,
		HttpOnly: true,
		Path:     "/",
	})
	user.GetExtensions().Del(SetCookieExt)
	user.GetExtensions().Del(RefreshTokenExpiresAtExt)
}

func respondWithAuthFailedError(w http.ResponseWriter, err error) {
	log.Tracef("Authentication failed: %+v", err)
	customErr := &exception.CustomError{
		Status:  http.StatusUnauthorized,
		Message: http.StatusText(http.StatusUnauthorized),
		Debug:   fmt.Sprintf("%v", err),
	}
	utils.RespondWithJson(w, customErr.Status, customErr)
}
