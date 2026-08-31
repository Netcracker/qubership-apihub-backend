package security

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/jwt"
	_ "github.com/shaj13/libcache/fifo"
	_ "github.com/shaj13/libcache/lru"
)

const LocalRefreshPath = "/api/v3/auth/local/refresh"
const gitIntegrationExt = "gitIntegration"

type UserView struct {
	AccessToken string    `json:"token"`
	RenewToken  string    `json:"renewToken"`
	User        view.User `json:"user"`
}

func (a *AuthHandler) CreateLocalUserToken_deprecated(w http.ResponseWriter, r *http.Request) {
	user, err := a.authenticateUser(r)
	if err != nil {
		a.respondWithAuthFailedError(w, err)
		return
	}
	userView, err := a.CreateTokenForUser_deprecated(*user)
	if err != nil {
		a.respondWithAuthFailedError(w, err)
		return
	}

	response, _ := json.Marshal(userView)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(response)
}

func (a *AuthHandler) CreateTokenForUser_deprecated(dbUser view.User) (*UserView, error) {
	accessToken, refreshToken, err := a.issueTokenPair(dbUser, true)
	if err != nil {
		return nil, err
	}

	userView := UserView{AccessToken: accessToken, RenewToken: refreshToken, User: dbUser}
	return &userView, nil
}

func (a *AuthHandler) CreateLocalUserToken(w http.ResponseWriter, r *http.Request) {
	user, err := a.authenticateUser(r)
	if err != nil {
		a.respondWithAuthFailedError(w, err)
		return
	}

	if err = a.SetAuthTokenCookies(w, user, LocalRefreshPath); err != nil {
		a.respondWithAuthFailedError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (a *AuthHandler) authenticateUser(r *http.Request) (*view.User, error) {
	email, password, ok := r.BasicAuth()
	if !ok {
		return nil, fmt.Errorf("user credentials are not provided")
	}
	user, err := a.userService.AuthenticateUser(email, password)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (a *AuthHandler) SetAuthTokenCookies(w http.ResponseWriter, user *view.User, refreshTokenPath string) error {
	accessToken, refreshToken, err := a.issueTokenPair(*user, false)
	if err != nil {
		return fmt.Errorf("failed to create token pair for user: %v", err.Error())
	}

	http.SetCookie(w, &http.Cookie{
		Name:     AccessTokenCookieName,
		Value:    accessToken,
		MaxAge:   int(a.accessTokenDuration.Seconds()),
		Secure:   a.productionMode,
		HttpOnly: true,
		Path:     "/",
	})
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    refreshToken,
		MaxAge:   int(a.refreshTokenDuration.Seconds()),
		Secure:   a.productionMode,
		HttpOnly: true,
		Path:     refreshTokenPath,
	})
	return nil
}

func (a *AuthHandler) issueTokenPair(dbUser view.User, withGitIntegration bool) (accessToken string, refreshToken string, err error) {
	user := auth.NewUserInfo(dbUser.Name, dbUser.Id, []string{}, auth.Extensions{})
	accessDuration := jwt.SetExpDuration(a.accessTokenDuration) // should be more than one minute!

	extensions := user.GetExtensions()
	systemRole, err := a.roleService.GetUserSystemRole(user.GetID())
	if err != nil {
		return "", "", fmt.Errorf("failed to check user system role: %v", err.Error())
	}
	if systemRole != "" {
		extensions.Set(context.SystemRoleExt, systemRole)
	}
	if withGitIntegration {
		extensions.Set(gitIntegrationExt, "false") //TODO: can we remove it ?
	}
	user.SetExtensions(extensions)

	extensions.Set(TokenTypeExt, AccessTokenType)
	accessToken, err = jwt.IssueAccessToken(user, a.keeper, accessDuration)
	if err != nil {
		return "", "", err
	}

	extensions.Set(TokenTypeExt, RefreshTokenType)
	refreshDuration := jwt.SetExpDuration(a.refreshTokenDuration)
	refreshToken, err = jwt.IssueAccessToken(user, a.keeper, refreshDuration)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (a *AuthHandler) GetPublicKey() []byte {
	return a.publicKey
}
