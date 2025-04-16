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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security/cookie"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security/idp"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	log "github.com/sirupsen/logrus"
	"net/http"
	"net/url"
	"strings"
)

const (
	samlAttributeEmail      string = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"
	samlAttributeName       string = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname"
	samlAttributeSurname    string = "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname"
	samlAttributeUserAvatar string = "thumbnailPhoto"
	samlAttributeUserId     string = "User-Principal-Name"
)

type AuthController interface {
	AssertionConsumerHandler(w http.ResponseWriter, r *http.Request)
	StartAuthentication(w http.ResponseWriter, r *http.Request)
	ServeMetadata(w http.ResponseWriter, r *http.Request)
}

func NewAuthController(userService service.UserService, systemInfoService service.SystemInfoService, idpManager idp.IDPManager) AuthController {
	return &authControllerImpl{
		idpManager:        idpManager,
		userService:       userService,
		systemInfoService: systemInfoService,
	}
}

type authControllerImpl struct {
	idpManager        idp.IDPManager
	userService       service.UserService
	systemInfoService service.SystemInfoService
}

func (a *authControllerImpl) ServeMetadata(w http.ResponseWriter, r *http.Request) {
	idpId := getStringParam(r, "idpId")
	provider, exists := a.idpManager.GetProvider(idpId)
	if exists {
		serveMetadata(w, r, provider.Saml)
	} else {
		serveMetadata(w, r, nil)
	}
}

func serveMetadata(w http.ResponseWriter, r *http.Request, samlInstance *samlsp.Middleware) {
	if samlInstance == nil {
		log.Errorf("Cannot serveMetadata with nil samlInstanse")
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Code:    exception.SamlInstanceIsNull,
			Message: exception.SamlInstanceIsNullMsg,
			Params:  map[string]interface{}{"error": "saml instance is not initialized"},
		})
		return
	}
	samlInstance.ServeMetadata(w, r)
}

// StartAuthentication Frontend calls this endpoint to SSO login user via SAML
func (a *authControllerImpl) StartAuthentication(w http.ResponseWriter, r *http.Request) {
	idpId := r.URL.Query().Get("idpId")
	provider, exists := a.idpManager.GetProvider(idpId)
	if exists {
		startSAMLAuthentication(w, r, provider.Saml, a.systemInfoService.GetAllowedHosts())
	} else {
		startSAMLAuthentication(w, r, nil, nil)
	}
}

func startSAMLAuthentication(w http.ResponseWriter, r *http.Request, samlInstance *samlsp.Middleware, allowedHosts []string) {
	if samlInstance == nil {
		log.Errorf("Cannot StartSamlAuthentication with nil samlInstance")
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Code:    exception.SamlInstanceIsNull,
			Message: exception.SamlInstanceIsNullMsg,
			Params:  map[string]interface{}{"error": "saml instance is not initialized"},
		})
		return
	}
	redirectUrlStr := r.URL.Query().Get("redirectUri")

	log.Debugf("redirect url - %s", redirectUrlStr)

	redirectUrl, err := url.Parse(redirectUrlStr)
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.IncorrectRedirectUrlError,
			Message: exception.IncorrectRedirectUrlErrorMsg,
			Params:  map[string]interface{}{"error": err.Error()},
		})
		return
	}
	var validHost bool
	for _, host := range allowedHosts {
		if strings.Contains(redirectUrl.Host, host) {
			validHost = true
			break
		}
	}
	if !validHost {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.HostNotAllowed,
			Message: exception.HostNotAllowedMsg,
			Params:  map[string]interface{}{"host": redirectUrlStr},
		})
		return
	}

	//Current URL is something like /api/v2/auth/saml, and it's a dedicated login endpoint.
	//Frontend detects missing/bad/expired token by 401 response and goes to the endpoint itself with redirectUri as a parameter.
	//But saml library is using middleware logic, i.e. it expects that client is trying to call some business endpoint and checks the security.
	//SAML library stores original URL and after successful auth redirects to it.
	//This is a different flow that we have. Changing r.URL to redirectUrl allows us to adapt to library's middleware flow, it will redirect to expected endpoint automatically.
	r.URL = redirectUrl

	// Note that we do not use built-in session mechanism from saml lib except request tracking cookie
	samlInstance.HandleStartAuthFlow(w, r)
}

// AssertionConsumerHandler This endpoint is called by ADFS when auth procedure is complete on it's side. ADFS posts the response here.
func (a *authControllerImpl) AssertionConsumerHandler(w http.ResponseWriter, r *http.Request) {
	idpId := getStringParam(r, "idpId")
	provider, exists := a.idpManager.GetProvider(idpId)
	if exists {
		handleAssertion(w, r, provider.Saml, a.setApihubSessionCookie)
	} else {
		handleAssertion(w, r, nil, nil)
	}
}

func handleAssertion(w http.ResponseWriter, r *http.Request, samlInstance *samlsp.Middleware, setAuthCookie func(w http.ResponseWriter, assertion *saml.Assertion)) {
	if samlInstance == nil {
		log.Errorf("Cannot run AssertionConsumerHandler with nill samlInstanse")
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Code:    exception.SamlInstanceIsNull,
			Message: exception.SamlInstanceIsNullMsg,
			Params:  map[string]interface{}{"error": "saml instance is not initialized"},
		})
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse ACS form: %s", err), http.StatusBadRequest)
		return
	}
	possibleRequestIDs := []string{}
	if samlInstance.ServiceProvider.AllowIDPInitiated {
		possibleRequestIDs = append(possibleRequestIDs, "")
	}
	trackedRequests := samlInstance.RequestTracker.GetTrackedRequests(r)
	for _, tr := range trackedRequests {
		possibleRequestIDs = append(possibleRequestIDs, tr.SAMLRequestID)
	}
	assertion, err := samlInstance.ServiceProvider.ParseResponse(r, possibleRequestIDs)
	if err != nil {
		log.Errorf("Parsing SAML response process error: %s", err.Error())
		var ire *saml.InvalidResponseError
		if errors.As(err, &ire) {
			log.Errorf("Parsing SAML response process private error: %s", ire.PrivateErr.Error())
			log.Debugf("ACS response data: %s", ire.Response)
		}
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Code:    exception.SamlResponseHasParsingError,
			Message: exception.SamlResponseHasParsingErrorMsg,
			Params:  map[string]interface{}{"error": err.Error()},
		})
		return
	}
	if assertion == nil {
		log.Errorf("Assertion from SAML response is nil")
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Code:    exception.AssertionIsNull,
			Message: exception.AssertionIsNullMsg,
		})
		return
	}

	// Add Apihub auth info cookie
	setAuthCookie(w, assertion)

	// Extract original redirect URI from request tracking cookie
	redirectURI := "/"
	if trackedRequestIndex := r.Form.Get("RelayState"); trackedRequestIndex != "" {
		log.Debugf("trackedRequestIndex = %s", trackedRequestIndex)
		trackedRequest, err := samlInstance.RequestTracker.GetTrackedRequest(r, trackedRequestIndex)
		if err != nil {
			if errors.Is(err, http.ErrNoCookie) && samlInstance.ServiceProvider.AllowIDPInitiated {
				if uri := r.Form.Get("RelayState"); uri != "" {
					redirectURI = uri
					log.Debugf("redirectURI is found in RelayState and updated to %s", redirectURI)
				}
			}
			utils.RespondWithError(w, "Unable to retrieve redirect URL: failed to get tracked request", err)
			return
		} else {
			err = samlInstance.RequestTracker.StopTrackingRequest(w, r, trackedRequestIndex)
			if err != nil {
				log.Warnf("Failed to stop tracking request: %s", err)
				// but it's not a showstopper, so continue processing
			}
			redirectURI = trackedRequest.URI
			log.Debugf("redirectURI is found in trackedRequest and updated to %s", redirectURI)
		}
	}

	http.Redirect(w, r, redirectURI, http.StatusFound)
}

func (a *authControllerImpl) setApihubSessionCookie(w http.ResponseWriter, assertion *saml.Assertion) {
	assertionAttributes := getAssertionAttributes(assertion)

	user, err := getOrCreateUser(a.userService, assertionAttributes)
	if err != nil {
		utils.RespondWithError(w, "Failed to get or create SSO user", err)
		return
	}

	authCookie, err := security.CreateTokenForUser(*user)
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Message: "Failed to create token for SSO user",
			Debug:   err.Error(),
		})
		return
	}

	response, _ := json.Marshal(authCookie)
	cookieValue := base64.StdEncoding.EncodeToString(response)

	http.SetCookie(w, &http.Cookie{
		Name:     cookie.SessionCookieName,
		Value:    cookieValue,
		MaxAge:   a.systemInfoService.GetRefreshTokenDurationSec(),
		Secure:   false, //TODO: set to true
		HttpOnly: true,
		Path:     "/",
	})
	log.Debugf("Auth user result object: %+v", authCookie)
}

func getAssertionAttributes(assertion *saml.Assertion) map[string][]string {
	assertionAttributes := make(map[string][]string)
	for _, attributeStatement := range assertion.AttributeStatements {
		for _, attr := range attributeStatement.Attributes {
			claimName := attr.FriendlyName
			if claimName == "" {
				claimName = attr.Name
			}
			for _, value := range attr.Values {
				assertionAttributes[claimName] = append(assertionAttributes[claimName], value.Value)
			}
		}
	}
	return assertionAttributes
}

func getOrCreateUser(userService service.UserService, assertionAttributes map[string][]string) (*view.User, error) {
	samlUser := view.User{}
	if len(assertionAttributes[samlAttributeUserId]) != 0 {
		userLogin := assertionAttributes[samlAttributeUserId][0]
		if strings.Contains(userLogin, "@") {
			samlUser.Id = strings.Split(assertionAttributes[samlAttributeUserId][0], "@")[0]
		} else {
			samlUser.Id = userLogin
		}
		log.Debugf("Attributes from saml response for user %s - %v", samlUser.Id, assertionAttributes)
	} else {
		log.Error("UserId is empty in saml response")
		log.Errorf("Attributes from saml response - %v", assertionAttributes)
		return nil, &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Code:    exception.SamlResponseHaveNoUserId,
			Message: exception.SamlResponseHaveNoUserIdMsg,
		}
	}

	if len(assertionAttributes[samlAttributeName]) != 0 {
		samlUser.Name = assertionAttributes[samlAttributeName][0]
	}
	if len(assertionAttributes[samlAttributeSurname]) != 0 {
		samlUser.Name = fmt.Sprintf("%s %s", samlUser.Name, assertionAttributes[samlAttributeSurname][0])
	}
	if len(assertionAttributes[samlAttributeEmail]) != 0 {
		samlUser.Email = assertionAttributes[samlAttributeEmail][0]
	} else {
		log.Error("Email is empty in saml response")
		log.Errorf("Attributes from saml response - %v", assertionAttributes)
		return nil, &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Code:    exception.SamlResponseMissingEmail,
			Message: exception.SamlResponseMissingEmailMsg,
		}
	}

	if len(assertionAttributes[samlAttributeUserAvatar]) != 0 {
		samlUser.AvatarUrl = fmt.Sprintf("/api/v2/users/%s/profile/avatar", samlUser.Id)
		avatar := assertionAttributes[samlAttributeUserAvatar][0]

		decodedAvatar, err := base64.StdEncoding.DecodeString(avatar)
		if err != nil {
			log.Errorf("Failed to decode user avatar during SSO user login: %s", err)
			return nil, &exception.CustomError{
				Status:  http.StatusInternalServerError,
				Code:    exception.SamlResponseHasBrokenContent,
				Message: exception.SamlResponseHasBrokenContentMsg,
				Params:  map[string]interface{}{"userId": samlUser.Id, "error": err.Error()},
				Debug:   "Failed to decode user avatar",
			}
		}
		err = userService.StoreUserAvatar(samlUser.Id, decodedAvatar)
		if err != nil {
			return nil, fmt.Errorf("failed to store user avatar: %w", err)
		}
	}

	user, err := userService.GetOrCreateUserForIntegration(samlUser, view.ExternalSamlIntegration)
	if err != nil {
		return nil, fmt.Errorf("failed to create user for SSO integration: %w", err)
	}

	return user, nil
}
