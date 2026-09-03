package security

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/responder"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/jwt"
	"github.com/shaj13/go-guardian/v2/auth/strategies/union"
	"github.com/shaj13/libcache"
	log "github.com/sirupsen/logrus"
)

type AuthHandler struct {
	responder            *responder.Responder
	userService          service.UserService
	roleService          service.RoleService
	accessTokenDuration  time.Duration
	refreshTokenDuration time.Duration
	productionMode       bool
	jwtValidator         JWTValidator
	refreshTokenStrategy auth.Strategy
	fullAuthStrategy     union.Union
	userAuthStrategy     union.Union
	jwtAuthStrategy      union.Union
	proxyAuthStrategy    union.Union
	apiKeyStrategy       auth.Strategy
	publicKey            []byte
	keeper               jwt.SecretsKeeper
}

func NewAuthHandler(userService service.UserService, roleService service.RoleService, apiKeyService service.ApihubApiKeyService, patService service.PersonalAccessTokenService, systemInfoService service.SystemInfoService, tokenRevocationService service.TokenRevocationService, responder *responder.Responder) (*AuthHandler, error) {
	apihubApiKeyStrategy := NewApihubApiKeyStrategy(apiKeyService)
	personalAccessTokenStrategy := NewApihubPATStrategy(patService)
	accessTokenDuration := time.Second * time.Duration(systemInfoService.GetAccessTokenDurationSec())
	refreshTokenDuration := time.Second * time.Duration(systemInfoService.GetRefreshTokenDurationSec())
	productionMode := systemInfoService.IsProductionMode()

	block, _ := pem.Decode(systemInfoService.GetJwtPrivateKey())
	pkcs8PrivateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("can't parse pkcs1 private key. Error - %s", err.Error())
	}
	privateKey, ok := pkcs8PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("can't parse pkcs8 private key to rsa.PrivateKey. Error - %s", err.Error())
	}
	keySize := privateKey.N.BitLen()
	if keySize < 2048 || keySize > 4096 {
		return nil, fmt.Errorf("RSA key length must be between 2048 and 4096 bits, got %d bits", keySize)
	}
	publicKey := x509.MarshalPKCS1PublicKey(&privateKey.PublicKey)

	keeper := jwt.StaticSecret{
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
	refreshTokenStrategy := NewRefreshTokenStrategy(cache, jwtValidator, accessTokenDuration, keeper)
	fullAuthStrategy := union.New(bearerTokenStrategy, cookieTokenStrategy, apihubApiKeyStrategy, personalAccessTokenStrategy)
	userAuthStrategy := union.New(bearerTokenStrategy, cookieTokenStrategy, personalAccessTokenStrategy)
	jwtAuthStrategy := union.New(bearerTokenStrategy, cookieTokenStrategy)
	customJwtStrategy := NewCustomJWTStrategy(cache, jwtValidator)
	proxyAuthStrategy := union.New(customJwtStrategy, cookieTokenStrategy)
	apiKeyStrategy := apihubApiKeyStrategy
	return &AuthHandler{
		responder:            responder,
		userService:          userService,
		roleService:          roleService,
		accessTokenDuration:  accessTokenDuration,
		refreshTokenDuration: refreshTokenDuration,
		productionMode:       productionMode,
		jwtValidator:         jwtValidator,
		refreshTokenStrategy: refreshTokenStrategy,
		fullAuthStrategy:     fullAuthStrategy,
		userAuthStrategy:     userAuthStrategy,
		jwtAuthStrategy:      jwtAuthStrategy,
		proxyAuthStrategy:    proxyAuthStrategy,
		apiKeyStrategy:       apiKeyStrategy,
		publicKey:            publicKey,
		keeper:               keeper,
	}, nil
}

func (a *AuthHandler) respondWithAuthFailedError(w http.ResponseWriter, r *http.Request, err error) {
	if cause := a.contextErrorCause(err); cause != nil {
		// A request that gave up while authenticating is not an authentication failure, and reporting it
		// as 401 sends the user off to fix credentials that can be perfectly valid.
		a.responder.RespondWithContextError(w, r, "Authentication aborted", cause, err)
		return
	}
	log.Tracef("Authentication failed: %+v", err)
	customErr := &exception.CustomError{
		Status:  http.StatusUnauthorized,
		Message: http.StatusText(http.StatusUnauthorized),
		Debug:   fmt.Sprintf("%v", err),
	}
	a.responder.RespondWithJson(w, customErr.Status, customErr)
}
