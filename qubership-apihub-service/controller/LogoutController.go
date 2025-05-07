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

package controller

import (
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"net/http"
	"time"
)

type LogoutController interface {
	Logout(http.ResponseWriter, *http.Request)
}

func NewLogoutController(tokenRevocationService service.TokenRevocationService, systemInfoService service.SystemInfoService) LogoutController {
	authConfig := systemInfoService.GetAuthConfig()
	//TODO: perhaps it is better to add refresh token path to the access token extensions
	var refreshTokenPaths []string
	for _, idp := range authConfig.Providers {
		if idp.RefreshTokenEndpoint != "" {
			refreshTokenPaths = append(refreshTokenPaths, idp.RefreshTokenEndpoint)
		} else if idp.LoginStartEndpoint != "" {
			refreshTokenPaths = append(refreshTokenPaths, idp.LoginStartEndpoint)
		}
	}

	return &logoutControllerImpl{tokenRevocationService: tokenRevocationService, refreshTokenPaths: refreshTokenPaths}
}

type logoutControllerImpl struct {
	tokenRevocationService service.TokenRevocationService
	refreshTokenPaths      []string
}

func (l *logoutControllerImpl) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := context.Create(r)
	err := l.tokenRevocationService.RevokeUserTokens(ctx.GetUserId())
	if err != nil {
		utils.RespondWithError(w, "Failed to perform user logout", err)
		return
	}

	// Clear access token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     security.AccessTokenCookieName,
		Value:    "",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		Secure:   true,
		HttpOnly: true,
		Path:     "/",
	})

	// Clear refresh token cookie
	for _, path := range l.refreshTokenPaths {
		http.SetCookie(w, &http.Cookie{
			Name:     security.RefreshTokenCookieName,
			Value:    "",
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
			Secure:   true,
			HttpOnly: true,
			Path:     path,
		})
	}

	w.WriteHeader(http.StatusNoContent)
}
