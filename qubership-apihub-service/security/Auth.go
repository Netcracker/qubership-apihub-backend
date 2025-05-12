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
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/jwt"
	"github.com/shaj13/go-guardian/v2/auth/strategies/union"
	"github.com/shaj13/libcache"
	_ "github.com/shaj13/libcache/fifo"
	_ "github.com/shaj13/libcache/lru"

	"time"
)

var fullAuthStrategy union.Union
var userAuthStrategy union.Union
var proxyAuthStrategy union.Union
var jwtAuthStrategy union.Union
var refreshTokenStrategy auth.Strategy

var keeper jwt.SecretsKeeper
var userService service.UserService
var roleService service.RoleService

var accessTokenDuration time.Duration
var refreshTokenDuration time.Duration

var publicKey []byte

func SetupGoGuardian(userServiceLocal service.UserService, roleServiceLocal service.RoleService, apiKeyService service.ApihubApiKeyService, patService service.PersonalAccessTokenService, systemInfoService service.SystemInfoService, tokenRevocationService service.TokenRevocationService) error {
	userService = userServiceLocal
	roleService = roleServiceLocal
	apihubApiKeyStrategy := NewApihubApiKeyStrategy(apiKeyService)
	personalAccessTokenStrategy := NewApihubPATStrategy(patService)
	accessTokenDuration = time.Second * time.Duration(systemInfoService.GetAccessTokenDurationSec())
	refreshTokenDuration = time.Second * time.Duration(systemInfoService.GetRefreshTokenDurationSec())

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

	cache := libcache.LRU.New(2000)
	cache.RegisterOnExpired(func(key, _ interface{}) {
		cache.Delete(key)
	})
	jwtValidator := NewJWTValidator(keeper, tokenRevocationService)
	bearerTokenStrategy := NewBearerTokenStrategy(cache, jwtValidator)
	cookieTokenStrategy := NewCookieTokenStrategy(cache, jwtValidator)
	refreshTokenStrategy = NewRefreshTokenStrategy(cache, jwtValidator)
	fullAuthStrategy = union.New(bearerTokenStrategy, cookieTokenStrategy, apihubApiKeyStrategy, personalAccessTokenStrategy)
	userAuthStrategy = union.New(bearerTokenStrategy, cookieTokenStrategy, personalAccessTokenStrategy)
	jwtAuthStrategy = union.New(bearerTokenStrategy, cookieTokenStrategy)
	customJwtStrategy := NewCustomJWTStrategy(cache, jwtValidator)
	proxyAuthStrategy = union.New(customJwtStrategy, cookieTokenStrategy)

	return nil
}

type UserView struct {
	AccessToken string    `json:"token"`
	RenewToken  string    `json:"renewToken"`
	User        view.User `json:"user"`
}

func CreateLocalUserToken_deprecated(w http.ResponseWriter, r *http.Request) {
	user, err := authenticateUser(r)
	if err != nil {
		respondWithAuthFailedError(w, err)
		return
	}
	userView, err := CreateTokenForUser_deprecated(*user)
	if err != nil {
		respondWithAuthFailedError(w, err)
		return
	}

	response, _ := json.Marshal(userView)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(response)
}

func CreateTokenForUser_deprecated(dbUser view.User) (*UserView, error) {
	accessToken, refreshToken, err := issueTokenPair(dbUser)
	if err != nil {
		return nil, err
	}

	userView := UserView{AccessToken: accessToken, RenewToken: refreshToken, User: dbUser}
	return &userView, nil
}

func CreateLocalUserToken(w http.ResponseWriter, r *http.Request) {
	user, err := authenticateUser(r)
	if err != nil {
		respondWithAuthFailedError(w, err)
		return
	}

	if err = SetAuthTokenCookies(w, user, "/api/v3/auth/local/refresh"); err != nil {
		respondWithAuthFailedError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func authenticateUser(r *http.Request) (*view.User, error) {
	email, password, ok := r.BasicAuth()
	if !ok {
		return nil, fmt.Errorf("user credentials are not provided")
	}
	user, err := userService.AuthenticateUser(email, password)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func SetAuthTokenCookies(w http.ResponseWriter, user *view.User, refreshTokenPath string) error {
	accessToken, refreshToken, err := issueTokenPair(*user)
	if err != nil {
		return fmt.Errorf("failed to create token pair for user: %v", err.Error())
	}

	http.SetCookie(w, &http.Cookie{
		Name:     AccessTokenCookieName,
		Value:    accessToken,
		MaxAge:   int(accessTokenDuration.Seconds()),
		Secure:   true,
		HttpOnly: true,
		Path:     "/",
	})
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    refreshToken,
		MaxAge:   int(refreshTokenDuration.Seconds()),
		Secure:   true,
		HttpOnly: true,
		Path:     refreshTokenPath,
	})
	return nil
}

func issueTokenPair(dbUser view.User) (accessToken string, refreshToken string, err error) {
	user := auth.NewUserInfo(dbUser.Name, dbUser.Id, []string{}, auth.Extensions{})
	accessDuration := jwt.SetExpDuration(accessTokenDuration) // should be more than one minute!

	extensions := user.GetExtensions()
	systemRole, err := roleService.GetUserSystemRole(user.GetID())
	if err != nil {
		return "", "", fmt.Errorf("failed to check user system role: %v", err.Error())
	}
	if systemRole != "" {
		extensions.Set(context.SystemRoleExt, systemRole)
	}
	user.SetExtensions(extensions)

	accessToken, err = jwt.IssueAccessToken(user, keeper, accessDuration)
	if err != nil {
		return "", "", err
	}

	refreshDuration := jwt.SetExpDuration(refreshTokenDuration)
	refreshToken, err = jwt.IssueAccessToken(user, keeper, refreshDuration)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func GetPublicKey() []byte {
	return publicKey
}
