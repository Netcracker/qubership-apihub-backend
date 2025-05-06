package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security/idp"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/coreos/go-oidc/v3/oidc"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"net/http"
	"time"
)

type oidcProvider struct {
	config       idp.IDP
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oAuth2Config oauth2.Config
	allowedHosts []string
}

func newOIDCProvider(config idp.IDP, provider *oidc.Provider, verifier *oidc.IDTokenVerifier, oAuth2Config oauth2.Config, allowedHosts []string) idp.Provider {
	return &oidcProvider{
		config:       config,
		provider:     provider,
		verifier:     verifier,
		oAuth2Config: oAuth2Config,
		allowedHosts: allowedHosts,
	}
}

func (o oidcProvider) GetId() string {
	return o.config.Id
}

func (o oidcProvider) StartAuthentication(w http.ResponseWriter, r *http.Request) {
	redirectUrlStr := r.URL.Query().Get("redirectUri")

	log.Debugf("redirect url - %s", redirectUrlStr)

	if _, err := utils.ParseAndValidateRedirectURL(redirectUrlStr, o.allowedHosts); err != nil {
		utils.RespondWithCustomError(w, err)
		return
	}

	state, err := o.generateState()
	if err != nil {
		utils.RespondWithError(w, "Failed to generate state", err)
		return
	}
	nonce, err := o.generateNonce()
	if err != nil {
		utils.RespondWithError(w, "Failed to generate nonce", err)
		return
	}
	codeVerifier, err := o.generateCodeVerifier()
	if err != nil {
		utils.RespondWithError(w, "Failed to generate code verifier", err)
		return
	}
	codeChallenge := o.generateCodeChallenge(codeVerifier)

	o.setCallbackCookie(w, "oidc_state_"+o.config.Id, state)
	o.setCallbackCookie(w, "oidc_nonce_"+o.config.Id, nonce)
	o.setCallbackCookie(w, "oidc_code_verifier_"+o.config.Id, codeVerifier)
	o.setCallbackCookie(w, "oidc_redirect_"+o.config.Id, redirectUrlStr)

	authURL := o.oAuth2Config.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (o oidcProvider) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("oidc_state_" + o.config.Id)
	if err != nil {
		utils.RespondWithError(w, "State cookie not found", err)
		return
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		utils.RespondWithError(w, "Invalid state parameter", errors.New("state mismatch"))
		return
	}

	nonceCookie, err := r.Cookie("oidc_nonce_" + o.config.Id)
	if err != nil {
		utils.RespondWithError(w, "Nonce cookie not found", err)
		return
	}

	codeVerifierCookie, err := r.Cookie("oidc_code_verifier_" + o.config.Id)
	if err != nil {
		utils.RespondWithError(w, "Code verifier cookie not found", err)
		return
	}
	codeVerifier := codeVerifierCookie.Value

	redirectURI := "/"
	if redirectCookie, err := r.Cookie("oidc_redirect_" + o.config.Id); err == nil {
		redirectURI = redirectCookie.Value
	} else {
		log.Warnf("Redirect cookie not found, using default: %v", err)
	}

	utils.DeleteCookie(w, "oidc_state_"+o.config.Id, o.config.OIDCConfiguration.RedirectPath)
	utils.DeleteCookie(w, "oidc_nonce_"+o.config.Id, o.config.OIDCConfiguration.RedirectPath)
	utils.DeleteCookie(w, "oidc_code_verifier_"+o.config.Id, o.config.OIDCConfiguration.RedirectPath)
	utils.DeleteCookie(w, "oidc_redirect_"+o.config.Id, o.config.OIDCConfiguration.RedirectPath)

	oauth2Token, err := o.oAuth2Config.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(codeVerifier))
	if err != nil {
		utils.RespondWithError(w, "Failed to exchange token", err)
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		utils.RespondWithError(w, "No ID token found", errors.New("no id_token in OAuth2 token response"))
		return
	}

	idToken, err := o.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify ID token", err)
		return
	}

	if idToken.Nonce != nonceCookie.Value {
		utils.RespondWithError(w, "Invalid nonce", errors.New("nonce mismatch"))
		return
	}

	var claims idp.OIDCClaims
	if err := idToken.Claims(&claims); err != nil {
		utils.RespondWithError(w, "Failed to parse claims", err)
		return
	}

	//TODO: respond with custom error
	if claims.Username == "" {
		utils.RespondWithError(w, "Username is empty", errors.New("user id (preferred_username) is missing in OIDC claims"))
		return
	}
	if claims.Email == "" {
		utils.RespondWithError(w, "Email is empty", errors.New("email is missing in OIDC claims"))
		return
	}

	oidcUser := view.User{
		Id:    claims.Username,
		Name:  claims.Name,
		Email: claims.Email,
	}

	//TODO: is the URL publicly available
	if claims.Picture != "" {
		oidcUser.AvatarUrl = claims.Picture
	}

	user, err := userService.GetOrCreateUserForIntegration(oidcUser, view.ExternalIdpIntegration, o.config.Id)
	if err != nil {
		utils.RespondWithError(w, "Failed to create user for OIDC integration", err)
		return
	}

	// Add authentication cookies
	if err = setAuthTokenCookies(w, user, "/api/v1/login/sso/"+o.config.Id); err != nil {
		utils.RespondWithError(w, "Failed to set auth cookie", err)
		return
	}

	// Redirect to the original destination
	http.Redirect(w, r, redirectURI, http.StatusFound)
}

func (o oidcProvider) ServeMetadata(w http.ResponseWriter, r *http.Request) {
	utils.RespondWithError(w, "Not implemented", errors.New("not implemented"))
}

func (o oidcProvider) generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func (o oidcProvider) generateNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func (o oidcProvider) generateCodeVerifier() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (o oidcProvider) generateCodeChallenge(verifier string) string {
	h := sha256.New()
	h.Write([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (o oidcProvider) setCallbackCookie(w http.ResponseWriter, name, value string) {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   int(time.Hour.Seconds()),
		Secure:   true,
		HttpOnly: true,
		Path:     o.config.OIDCConfiguration.RedirectPath,
	}
	http.SetCookie(w, c)
}
