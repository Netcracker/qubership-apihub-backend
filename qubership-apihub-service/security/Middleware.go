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
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/controller"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/union"
	log "github.com/sirupsen/logrus"
	"net/http"
	"runtime/debug"
)

func Secure(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("Request failed with panic: %v", err)
				log.Tracef("Stacktrace: %v", string(debug.Stack()))
				debug.PrintStack()
				controller.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		_, user, err := defaultAuthStrategy.AuthenticateRequest(r)
		if err != nil {
			if multiError, ok := err.(union.MultiError); ok {
				for _, e := range multiError {
					if customError, ok := e.(*exception.CustomError); ok {
						if customError.Status == http.StatusForbidden {
							controller.RespondWithCustomError(w, customError)
							return
						}
					}
				}
			}
			respondWithAuthFailedError(w, err)
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
				controller.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		//TODO: add PAT strategy and cookie strategy
		user, err := userAuthStrategy.Authenticate(r.Context(), r)
		if err != nil {
			respondWithAuthFailedError(w, err)
			return
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
				controller.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		//TODO: is it still required ?
		token := r.URL.Query().Get("token")
		r.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		_, user, err := defaultAuthStrategy.AuthenticateRequest(r)
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
				controller.RespondWithCustomError(w, &exception.CustomError{
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
				controller.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		//TODO: add cookie strategy
		user, err := proxyAuthStrategy.Authenticate(r.Context(), r)
		if err != nil {
			respondWithAuthFailedError(w, err)
			return
		}
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
				controller.RespondWithCustomError(w, &exception.CustomError{
					Status:  http.StatusInternalServerError,
					Message: http.StatusText(http.StatusInternalServerError),
					Debug:   fmt.Sprintf("%v", err),
				})
				return
			}
		}()
		//TODO: add cookie strategy
		user, err := proxyAuthStrategy.Authenticate(r.Context(), r)
		if err != nil {
			respondWithAuthFailedError(w, err)
			return
		}
		r = auth.RequestWithUser(user, r)
		r.Header.Del(CustomJwtAuthHeader)
		//TODO: do not forget to delete cookie from request
		cookies := r.Cookies()
		r.Header.Del("Cookie")
		for _, cookie := range cookies {
			if cookie.Name != view.SessionCookieName {
				r.AddCookie(cookie)
			}
		}
		next.ServeHTTP(w, r)
	}
}

func respondWithAuthFailedError(w http.ResponseWriter, err error) {
	log.Tracef("Authentication failed: %+v", err)
	customErr := &exception.CustomError{
		Status:  http.StatusUnauthorized,
		Message: http.StatusText(http.StatusUnauthorized),
		Debug:   fmt.Sprintf("%v", err),
	}
	controller.RespondWithJson(w, customErr.Status, customErr)
}
