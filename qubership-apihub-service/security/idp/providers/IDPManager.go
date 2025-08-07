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

package providers

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security/idp"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	dsig "github.com/russellhaering/goxmldsig"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

func NewIDPManager(authConfig idp.AuthConfig, allowedHosts []string, productionMode bool, userService service.UserService) (idp.Manager, error) {
	idpManager := idpManagerImpl{
		config:    authConfig,
		providers: make(map[string]idp.Provider),
	}
	for _, provider := range idpManager.config.Providers {
		if provider.Protocol == idp.AuthProtocolSAML {
			if _, exists := idpManager.providers[provider.Id]; exists {
				log.Debugf("SAML provider with id %s already exists", provider.Id)
				continue
			}
			samlProvider, err := idpManager.createSAMLProvider(provider, userService)
			if err != nil {
				return nil, err
			}
			idpManager.providers[provider.Id] = samlProvider
		} else if provider.Protocol == idp.AuthProtocolOIDC {
			if _, exists := idpManager.providers[provider.Id]; exists {
				log.Debugf("OIDC provider with id %s already exists", provider.Id)
				continue
			}
			oidcProvider, err := idpManager.createOIDCProvider(provider, userService, allowedHosts, productionMode)
			if err != nil {
				return nil, err
			}
			idpManager.providers[provider.Id] = oidcProvider
		}
	}
	return &idpManager, nil
}

type idpManagerImpl struct {
	config    idp.AuthConfig
	providers map[string]idp.Provider
}

func (i *idpManagerImpl) GetAuthConfig() idp.AuthConfig {
	return i.config
}

func (i *idpManagerImpl) GetProvider(id string) (idp.Provider, bool) {
	instance, exists := i.providers[id]
	return instance, exists
}

func (i *idpManagerImpl) IsSSOIntegrationEnabled() bool {
	return len(i.config.Providers) > 0
}

func (i *idpManagerImpl) createSAMLProvider(idpConfig idp.IDP, userService service.UserService) (idp.Provider, error) {
	samlInstance, err := CreateSAMLInstance(idpConfig.Id, idpConfig.SAMLConfiguration)
	if err != nil {
		return nil, err
	}
	rootURL, _ := url.Parse(idpConfig.SAMLConfiguration.RootURL)
	return newSAMLProvider(samlInstance, idpConfig, userService, rootURL.Hostname()), nil
}

func (i *idpManagerImpl) createOIDCProvider(idpConfig idp.IDP, userService service.UserService, allowedHosts []string, productionMode bool) (idp.Provider, error) {
	if idpConfig.OIDCConfiguration == nil {
		log.Error("OIDC configuration is invalid")
		return nil, fmt.Errorf("OIDC configuration is invalid")
	}

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, idpConfig.OIDCConfiguration.ProviderURL)
	if err != nil {
		log.Errorf("Failed to create OIDC provider: %v", err)
		return nil, err
	}

	rootURL, err := url.Parse(idpConfig.OIDCConfiguration.RootURL)
	if err != nil {
		log.Errorf("rootURL error - %s", err)
		return nil, err
	}

	oidcConfig := oauth2.Config{
		ClientID:     idpConfig.OIDCConfiguration.ClientID,
		ClientSecret: idpConfig.OIDCConfiguration.ClientSecret,
		RedirectURL:  idpConfig.OIDCConfiguration.RootURL + idpConfig.OIDCConfiguration.RedirectPath,
		Endpoint:     provider.Endpoint(),
		Scopes:       idpConfig.OIDCConfiguration.Scopes,
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: idpConfig.OIDCConfiguration.ClientID})
	return newOIDCProvider(idpConfig, provider, verifier, oidcConfig, userService, allowedHosts, rootURL.Hostname(), productionMode), nil
}

func CreateSAMLInstance(idpId string, samlConfig *idp.SAMLConfiguration) (*samlsp.Middleware, error) {
	if samlConfig == nil {
		log.Error("SAML configuration is invalid")
		return nil, fmt.Errorf("SAML configuration is invalid")
	}

	decodeSamlCert, err := base64.StdEncoding.DecodeString(samlConfig.Certificate)
	if err != nil {
		return nil, err
	}
	crt, err := os.CreateTemp("", "apihub.cert")
	if err != nil {
		return nil, fmt.Errorf("apihub.cert temp file wasn't created: %w", err)
	}
	defer func(name string) {
		if err := os.Remove(name); err != nil {
			log.Warnf("failed to remove temporary file %s: %v", name, err)
		}
	}(crt.Name())

	if _, err := crt.Write(decodeSamlCert); err != nil {
		return nil, fmt.Errorf("SAML_CRT write error: %w", err)
	}
	if err := crt.Close(); err != nil {
		return nil, fmt.Errorf("SAML_CRT close error: %w", err)
	}

	decodePrivateKey, err := base64.StdEncoding.DecodeString(samlConfig.PrivateKey)
	if err != nil {
		return nil, err
	}
	key, err := os.CreateTemp("", "apihub.key")
	if err != nil {
		return nil, fmt.Errorf("apihub.key temp file wasn't created: %w", err)
	}
	defer func(name string) {
		if err := os.Remove(name); err != nil {
			log.Warnf("failed to remove temporary file %s: %v", name, err)
		}
	}(key.Name())
	if _, err := key.Write(decodePrivateKey); err != nil {
		return nil, fmt.Errorf("SAML_KEY write error: %w", err)
	}
	if err := key.Close(); err != nil {
		return nil, fmt.Errorf("SAML_KEY close error: %w", err)
	}

	keyPair, err := tls.LoadX509KeyPair(crt.Name(), key.Name())
	if err != nil {
		return nil, fmt.Errorf("keyPair error: %w", err)
	}

	keyPair.Leaf, err = x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("keyPair.Leaf error: %w", err)
	}
	metadataUrl := samlConfig.IDPMetadataURL
	if metadataUrl == "" {
		return nil, fmt.Errorf("metadataUrl env is empty")
	}
	idpMetadataURL, err := url.Parse(metadataUrl)
	if err != nil {
		return nil, fmt.Errorf("idpMetadataURL error: %w", err)
	}

	tr := http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	cl := http.Client{Transport: &tr, Timeout: time.Second * 60}
	idpMetadata, err := samlsp.FetchMetadata(context.Background(), &cl, *idpMetadataURL)

	if err != nil {
		return nil, fmt.Errorf("idpMetadata error: %w", err)
	}
	rootURLPath := samlConfig.RootURL
	if rootURLPath == "" {
		return nil, fmt.Errorf("rootURLPath env is empty")
	}
	rootURL, err := url.Parse(rootURLPath)
	if err != nil {
		return nil, fmt.Errorf("rootURL error: %w", err)
	}

	samlSP, err := samlsp.New(samlsp.Options{
		URL:         *rootURL,
		Key:         keyPair.PrivateKey.(*rsa.PrivateKey),
		Certificate: keyPair.Leaf,
		IDPMetadata: idpMetadata,
		EntityID:    rootURL.Path,
	})
	if err != nil {
		return nil, fmt.Errorf("new saml instanse wasn't created: %w", err)
	}

	samlSP.ServiceProvider.SignatureMethod = dsig.RSASHA256SignatureMethod
	samlSP.ServiceProvider.AuthnNameIDFormat = saml.TransientNameIDFormat
	samlSP.ServiceProvider.AllowIDPInitiated = true
	if idpId != "" {
		samlSP.ServiceProvider.AcsURL = *rootURL.ResolveReference(&url.URL{Path: "api/v1/saml/" + idpId + "/acs"})
		samlSP.ServiceProvider.MetadataURL = *rootURL.ResolveReference(&url.URL{Path: "api/v1/saml/" + idpId + "/metadata"})
	}
	log.Infof("SAML instance initialized")
	return samlSP, nil
}
