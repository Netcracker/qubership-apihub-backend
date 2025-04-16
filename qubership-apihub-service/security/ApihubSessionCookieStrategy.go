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
	goctx "context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security/cookie"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/claims"
	"github.com/shaj13/go-guardian/v2/auth/strategies/jwt"
	"github.com/shaj13/libcache"
	"net/http"
	"strconv"
	"time"
)

const (
	SetCookieExt             = "setCookie"
	TokenIssuedAtExt         = "issuedAt"
	RefreshTokenExpiresAtExt = "refreshTokenExpiresAt"
)

func NewApihubSessionCookieStrategy(cache libcache.Cache, jwtValidator JWTValidator) auth.Strategy {
	return &apihubSessionCookieStrategyImpl{
		cache:        cache,
		jwtValidator: jwtValidator,
	}
}

type apihubSessionCookieStrategyImpl struct {
	cache        libcache.Cache
	jwtValidator JWTValidator
}

func (a apihubSessionCookieStrategyImpl) Authenticate(ctx goctx.Context, r *http.Request) (auth.Info, error) {
	sessionCookie, err := cookie.ExtractSessionCookie(r)
	if err != nil {
		return nil, err
	}
	if v, ok := a.cache.Load(sessionCookie.AccessToken); ok {
		info, ok := v.(auth.Info)
		if !ok {
			return nil, auth.NewTypeError("authentication failed:", (*auth.Info)(nil), v)
		}
		tokenCreationTimestamp, _ := strconv.ParseInt(info.GetExtensions().Get(TokenIssuedAtExt), 0, 64)
		if a.jwtValidator.IsTokenRevoked(info.GetID(), tokenCreationTimestamp) {
			return nil, fmt.Errorf("authentication failed: access token is revoked")
		}
		return info, nil
	}

	info, t, err := a.jwtValidator.ValidateToken(sessionCookie.AccessToken)
	if err != nil {
		if invalidErr, ok := err.(claims.InvalidError); ok {
			if invalidErr.Reason == claims.Expired {
				info, err := a.refreshToken(sessionCookie)
				if err != nil {
					return nil, err
				}
				return info, nil
			}
		}
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	a.cache.StoreWithTTL(sessionCookie.AccessToken, info, time.Until(t))

	return info, nil
}

// TODO: should we support other ways to transfer refresh token other than session cookie ?
func (a apihubSessionCookieStrategyImpl) refreshToken(sessionCookie *cookie.SessionCookie) (auth.Info, error) {
	var info auth.Info
	if v, ok := a.cache.Load(sessionCookie.RefreshToken); ok {
		info, ok = v.(auth.Info)
		if !ok {
			return nil, auth.NewTypeError("authentication failed:", (*auth.Info)(nil), v)
		}
		tokenCreationTimestamp, _ := strconv.ParseInt(info.GetExtensions().Get(TokenIssuedAtExt), 0, 64)
		if a.jwtValidator.IsTokenRevoked(info.GetID(), tokenCreationTimestamp) {
			return nil, fmt.Errorf("authentication failed: refresh token is revoked")
		}
	}
	if info == nil {
		var t time.Time
		var err error
		info, t, err = a.jwtValidator.ValidateToken(sessionCookie.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("authentication failed: %w", err)
		}
		a.cache.StoreWithTTL(sessionCookie.RefreshToken, info, time.Until(t))
	}

	userInfo, err := a.refreshAccessToken(info, sessionCookie.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("authentication failed, failed to refresh access token: %w", err)
	}

	return userInfo, nil
}

func (a apihubSessionCookieStrategyImpl) refreshAccessToken(userInfo auth.Info, refreshToken string) (auth.Info, error) {
	user := auth.NewUserInfo(userInfo.GetUserName(), userInfo.GetID(), []string{}, auth.Extensions{})
	extensions := user.GetExtensions()
	extensions.Set(context.SystemRoleExt, userInfo.GetExtensions().Get(context.SystemRoleExt))
	accessDuration := jwt.SetExpDuration(accessTokenDuration)
	exp := time.Now().UTC().Add(-claims.DefaultLeeway).Add(accessTokenDuration).Unix()

	newAccessToken, err := jwt.IssueAccessToken(user, keeper, accessDuration)
	if err != nil {
		return nil, err
	}

	sessionCookie := &cookie.SessionCookie{
		AccessToken:  newAccessToken,
		RefreshToken: refreshToken,
	}

	response, _ := json.Marshal(sessionCookie)
	cookieValue := base64.StdEncoding.EncodeToString(response)
	extensions.Set(SetCookieExt, cookieValue)
	extensions.Set(context.TokenExpiresAtExt, strconv.FormatInt(exp, 10))
	extensions.Set(RefreshTokenExpiresAtExt, userInfo.GetExtensions().Get(context.TokenExpiresAtExt))

	return user, nil
}
