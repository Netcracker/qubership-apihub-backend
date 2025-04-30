package security

import (
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
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

type samlProvider struct {
	samlInstance *samlsp.Middleware
	config       idp.IDP
}

func newSAMLProvider(samlInstance *samlsp.Middleware, config idp.IDP) idp.Provider {
	return &samlProvider{
		samlInstance: samlInstance,
		config:       config,
	}
}

func (s samlProvider) GetId() string {
	return s.config.Id
}

func (s samlProvider) StartAuthentication(w http.ResponseWriter, r *http.Request, allowedHosts []string) {
	StartSAMLAuthentication(w, r, s.samlInstance, allowedHosts)
}

func (s samlProvider) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	HandleAssertion(w, r, s.samlInstance, s.config.Id, setAuthTokenCookies)
}

func (s samlProvider) ServeMetadata(w http.ResponseWriter, r *http.Request) {
	ServeMetadata(w, r, s.samlInstance)
}

func StartSAMLAuthentication(w http.ResponseWriter, r *http.Request, samlInstance *samlsp.Middleware, allowedHosts []string) {
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

func HandleAssertion(w http.ResponseWriter, r *http.Request, samlInstance *samlsp.Middleware, providerId string, setAuthCookie func(w http.ResponseWriter, user *view.User, idpId string) error) {
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

	assertionAttributes := getAssertionAttributes(assertion)

	user, err := getOrCreateUser(userService, assertionAttributes, providerId)
	if err != nil {
		utils.RespondWithError(w, "Failed to get or create SSO user", err)
		return
	}

	// Add Apihub auth info cookie
	if err = setAuthCookie(w, user, providerId); err != nil {
		utils.RespondWithError(w, "Failed to set auth cookie", err)
		return
	}

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

func setAuthTokenCookies(w http.ResponseWriter, user *view.User, idpId string) error {
	accessToken, refreshToken, err := IssueTokenPair(*user)
	if err != nil {
		return &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Message: "Failed to create token for SSO user",
			Debug:   err.Error(),
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     AccessTokenCookieName,
		Value:    accessToken,
		MaxAge:   int(accessTokenDuration.Seconds()),
		Secure:   false, //TODO: should be true
		HttpOnly: true,
		Path:     "/",
	})
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    refreshToken,
		MaxAge:   int(refreshTokenDuration.Seconds()),
		Secure:   false, //TODO: should be true
		HttpOnly: true,
		Path:     "/api/v1/login/sso/" + idpId,
	})
	return nil
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

func getOrCreateUser(userService service.UserService, assertionAttributes map[string][]string, providerId string) (*view.User, error) {
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

	user, err := userService.GetOrCreateUserForIntegration(samlUser, view.ExternalIdpIntegration, providerId)
	if err != nil {
		return nil, fmt.Errorf("failed to create user for SSO integration: %w", err)
	}

	return user, nil
}

func ServeMetadata(w http.ResponseWriter, r *http.Request, samlInstance *samlsp.Middleware) {
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
