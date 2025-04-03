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
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/jwt"
	"github.com/shaj13/go-guardian/v2/auth/strategies/token"
	"github.com/shaj13/go-guardian/v2/auth/strategies/union"
	"github.com/shaj13/libcache"
	_ "github.com/shaj13/libcache/fifo"
	_ "github.com/shaj13/libcache/lru"

	"time"
)

var defaultAuthStrategy union.Union
var userAuthStrategy union.Union
var proxyAuthStrategy union.Union
var keeper jwt.SecretsKeeper
var userService service.UserService
var roleService service.RoleService
var systemInfoService service.SystemInfoService

var refreshTokenStrategy auth.Strategy

const CustomJwtAuthHeader = "X-Apihub-Authorization"

var publicKey []byte

func SetupGoGuardian(userServiceLocal service.UserService, roleServiceLocal service.RoleService, apiKeyService service.ApihubApiKeyService, patService service.PersonalAccessTokenService, systemService service.SystemInfoService) error {
	userService = userServiceLocal
	roleService = roleServiceLocal
	apihubApiKeyStrategy := NewApihubApiKeyStrategy(apiKeyService)
	personalAccessTokenStrategy := NewApihubPATStrategy(patService)
	systemInfoService = systemService

	block, _ := pem.Decode(systemInfoService.GetJwtPrivateKey())
	pkcs8PrivateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("can't parse pkcs1 private key. Error - %s", err.Error())
	}
	privateKey, ok := pkcs8PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return fmt.Errorf("can't parse pkcs8 private key to rsa.PrivateKey. Error - %s", err.Error())
	}
	publicKey = x509.MarshalPKCS1PublicKey(&privateKey.PublicKey)

	keeper = jwt.StaticSecret{
		ID:        "secret-id",
		Secret:    privateKey,
		Algorithm: jwt.RS256,
	}

	cache := libcache.LRU.New(1000)
	cache.SetTTL(time.Second * time.Duration(systemInfoService.GetAccessTokenDurationSec()))
	cache.RegisterOnExpired(func(key, _ interface{}) {
		cache.Delete(key)
	})
	bearerTokenStrategy := jwt.New(cache, keeper)
	sessionCookieStrategy := jwt.New(cache, keeper, token.SetParser(accessTokenCookieParser()))
	defaultAuthStrategy = union.New(bearerTokenStrategy, sessionCookieStrategy, apihubApiKeyStrategy, personalAccessTokenStrategy)
	userAuthStrategy = union.New(bearerTokenStrategy, sessionCookieStrategy, personalAccessTokenStrategy)
	customJwtStrategy := jwt.New(cache, keeper, token.SetParser(token.XHeaderParser(CustomJwtAuthHeader)))
	proxyAuthStrategy = union.New(customJwtStrategy, sessionCookieStrategy)

	refreshTokenCache := libcache.LRU.New(1000)
	refreshTokenCache.SetTTL(time.Second * time.Duration(systemInfoService.GetRefreshTokenDurationSec()))
	refreshTokenCache.RegisterOnExpired(func(key, _ interface{}) {
		refreshTokenCache.Delete(key)
	})
	refreshTokenStrategy = jwt.New(refreshTokenCache, keeper, token.SetParser(refreshTokenCookieParser()))
	return nil
}

type parser func(r *http.Request) (string, error)

func (p parser) Token(r *http.Request) (string, error) {
	return p(r)
}

func accessTokenCookieParser() token.Parser {
	tokenCookieParser := func(r *http.Request) (string, error) {
		authCookie, err := extractSessionCookie(r)
		if err != nil {
			return "", err
		}
		return authCookie.AccessToken, nil
	}

	return parser(tokenCookieParser)
}

func refreshTokenCookieParser() token.Parser {
	tokenCookieParser := func(r *http.Request) (string, error) {
		authCookie, err := extractSessionCookie(r)
		if err != nil {
			return "", err
		}
		return authCookie.RefreshToken, nil
	}

	return parser(tokenCookieParser)
}

func extractSessionCookie(r *http.Request) (*view.SessionCookie, error) {
	cookie, err := r.Cookie(view.SessionCookieName)
	if err != nil {
		return nil, fmt.Errorf("session cookie not found: %w", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode session cookie value: %w", err)
	}

	var session view.SessionCookie
	if err := json.Unmarshal(decoded, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session cookie: %w", err)
	}

	return &session, nil
}

func CreateLocalUserToken(w http.ResponseWriter, r *http.Request) {
	email, password, ok := r.BasicAuth()
	if !ok {
		respondWithAuthFailedError(w, fmt.Errorf("user credentials are not provided"))
		return
	}
	user, err := userService.AuthenticateUser(email, password)
	if err != nil {
		respondWithAuthFailedError(w, err)
		return
	}
	authCookie, err := CreateTokenForUser(*user)
	if err != nil {
		respondWithAuthFailedError(w, err)
		return
	}

	response, _ := json.Marshal(authCookie)
	cookieValue := base64.StdEncoding.EncodeToString(response)

	http.SetCookie(w, &http.Cookie{
		Name:     view.SessionCookieName,
		Value:    cookieValue,
		MaxAge:   systemInfoService.GetRefreshTokenDurationSec(),
		Secure:   true,
		HttpOnly: true,
		Path:     "/",
	})
	w.WriteHeader(http.StatusOK)
}

func CreateTokenForUser(dbUser view.User) (*view.SessionCookie, error) {
	user := auth.NewUserInfo(dbUser.Name, dbUser.Id, []string{}, auth.Extensions{})
	accessTokenDurationSec := systemInfoService.GetAccessTokenDurationSec()
	accessDuration := jwt.SetExpDuration(time.Second * time.Duration(accessTokenDurationSec)) // should be more than one minute!

	extensions := user.GetExtensions()
	systemRole, err := roleService.GetUserSystemRole(user.GetID())
	if err != nil {
		return nil, fmt.Errorf("failed to check user system role: %v", err.Error())
	}
	if systemRole != "" {
		extensions.Set(context.SystemRoleExt, systemRole)
	}
	user.SetExtensions(extensions)

	token, err := jwt.IssueAccessToken(user, keeper, accessDuration)

	if err != nil {
		return nil, err
	}

	refreshTokenDurationSec := systemInfoService.GetRefreshTokenDurationSec()
	refreshDuration := jwt.SetExpDuration(time.Second * time.Duration(refreshTokenDurationSec))
	refreshToken, err := jwt.IssueAccessToken(user, keeper, refreshDuration)
	if err != nil {
		return nil, err
	}

	authCookie := view.SessionCookie{AccessToken: token, RefreshToken: refreshToken}
	return &authCookie, nil
}

func RefreshToken(w http.ResponseWriter, r *http.Request) {
	// TODO: should we support other ways to transfer refresh token other than session cookie ?
	userInfo, err := refreshTokenStrategy.Authenticate(r.Context(), r)
	if err != nil {
		respondWithAuthFailedError(w, err)
		return
	}
	sessionCookie, err := extractSessionCookie(r)
	if err != nil {
		respondWithAuthFailedError(w, err)
		return
	}

	newSessionCookie, err := refreshAccessToken(userInfo, sessionCookie.RefreshToken)
	if err != nil {
		respondWithAuthFailedError(w, err)
		return
	}

	response, _ := json.Marshal(newSessionCookie)
	cookieValue := base64.StdEncoding.EncodeToString(response)

	http.SetCookie(w, &http.Cookie{
		Name:     view.SessionCookieName,
		Value:    cookieValue,
		MaxAge:   systemInfoService.GetRefreshTokenDurationSec(),
		Secure:   true,
		HttpOnly: true,
		Path:     "/",
	})

	w.WriteHeader(http.StatusOK)
}

func refreshAccessToken(userInfo auth.Info, refreshToken string) (*view.SessionCookie, error) {
	accessTokenDurationSec := systemInfoService.GetAccessTokenDurationSec()
	accessDuration := jwt.SetExpDuration(time.Second * time.Duration(accessTokenDurationSec))

	newAccessToken, err := jwt.IssueAccessToken(userInfo, keeper, accessDuration)
	if err != nil {
		return nil, err
	}

	return &view.SessionCookie{
		AccessToken:  newAccessToken,
		RefreshToken: refreshToken,
	}, nil
}

func GetPublicKey() []byte {
	return publicKey
}
