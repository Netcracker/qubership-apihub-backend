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
	"fmt"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/libcache"
	"net/http"
	"strconv"
	"time"
)

const (
	AccessTokenCookieName = "apihub-access-token"
	TokenIssuedAtExt      = "issuedAt"
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
	accessTokenCookie, err := r.Cookie(AccessTokenCookieName)
	if err != nil {
		return nil, fmt.Errorf("access token cookie not found")
	}
	accessToken := accessTokenCookie.Value
	if v, ok := a.cache.Load(accessToken); ok {
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

	info, t, err := a.jwtValidator.ValidateToken(accessToken)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	a.cache.StoreWithTTL(accessToken, info, time.Until(t))

	return info, nil
}
