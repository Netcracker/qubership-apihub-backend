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
	"crypto"
	"errors"
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/claims"
	"github.com/shaj13/go-guardian/v2/auth/strategies/jwt"
	jwt2 "gopkg.in/square/go-jose.v2/jwt"
	"strconv"
	"time"
)

type JWTValidator interface {
	ValidateToken(token string) (auth.Info, time.Time, error)
	IsTokenRevoked(userId string, tokenCreationTimestamp int64) bool
}

type jwtValidatorImpl struct {
	keeper                 jwt.SecretsKeeper
	tokenRevocationService service.TokenRevocationService
}

func NewJWTValidator(keeper jwt.SecretsKeeper, tokenRevocationService service.TokenRevocationService) JWTValidator {
	return &jwtValidatorImpl{
		keeper:                 keeper,
		tokenRevocationService: tokenRevocationService,
	}
}

func (j jwtValidatorImpl) IsTokenRevoked(userId string, tokenCreationTimestamp int64) bool {
	return j.tokenRevocationService.IsTokenRevoked(userId, tokenCreationTimestamp)
}

func (j jwtValidatorImpl) ValidateToken(token string) (auth.Info, time.Time, error) {
	c, info, err := j.parseAndValidate(token)
	if err != nil {
		return nil, time.Time{}, err
	}

	return info, time.Time(*c.ExpiresAt), nil
}

func (j jwtValidatorImpl) parseAndValidate(tstr string) (claims.Standard, auth.Info, error) {
	info := auth.NewUserInfo("", "", nil, make(auth.Extensions))
	c := claims.Standard{}
	opts := claims.VerifyOptions{
		Audience: claims.StringOrList{""},
		Issuer:   "",
		Time: func() (t time.Time) {
			// We don't need to add leeway when validating a token, as go-guardian already added leeway when issuing a token
			return time.Now().UTC()
		},
	}

	if err := j.parseToken(tstr, &c, info); err != nil {
		return claims.Standard{}, nil, err
	}

	if err := c.Verify(opts); err != nil {
		return claims.Standard{}, nil, err
	}

	if j.IsTokenRevoked(info.GetID(), time.Time(*c.IssuedAt).Unix()) {
		return claims.Standard{}, nil, fmt.Errorf("token is revoked")
	}

	info.GetExtensions().Set(TokenIssuedAtExt, strconv.FormatInt(time.Time(*c.IssuedAt).Unix(), 10))
	info.GetExtensions().Set(context.TokenExpiresAtExt, strconv.FormatInt(time.Time(*c.ExpiresAt).Unix(), 10))

	return c, info, nil
}

func (j jwtValidatorImpl) parseToken(token string, dest ...interface{}) error {
	jt, err := jwt2.ParseSigned(token)
	if err != nil {
		return err
	}

	if len(jt.Headers) == 0 {
		return errors.New("no headers found in JWT token")
	}

	if len(jt.Headers[0].KeyID) == 0 {
		return errors.New("token missing kid header")
	}

	secret, alg, err := j.keeper.Get(jt.Headers[0].KeyID)

	if err != nil {
		return err
	}

	if jt.Headers[0].Algorithm != alg {
		return errors.New("invalid signing algorithm, token alg header does not match key algorithm")
	}

	if v, ok := secret.(crypto.Signer); ok {
		secret = v.Public()
	}

	return jt.Claims(secret, dest...)
}
