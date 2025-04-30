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

package idp

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	dsig "github.com/russellhaering/goxmldsig"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"net/http"
	"net/url"
	"os"
	"time"
)

type IDPManager interface {
	GetAuthConfig() AuthConfig
	GetProvider(id string) (Provider, bool)
}

type ProviderFactory interface {
	NewSAMLProvider(samlInstance *samlsp.Middleware, config IDP, allowedHosts []string) Provider
	NewOIDCProvider(config IDP, provider *oidc.Provider, verifier *oidc.IDTokenVerifier, oAuth2Config oauth2.Config, allowedHosts []string) Provider
}

func NewIDPManager(authConfig AuthConfig, allowedHosts []string, factory ProviderFactory) (IDPManager, error) {
	idpManager := idpManagerImpl{
		config:    authConfig,
		providers: make(map[string]Provider),
		factory:   factory,
	}
	for _, idp := range idpManager.config.Providers {
		if idp.Protocol == AuthProtocolSAML {
			if _, exists := idpManager.providers[idp.Id]; exists {
				log.Debugf("SAML provider with id %s already exists", idp.Id)
				continue
			}
			provider, err := idpManager.createSAMLProvider(idp, allowedHosts)
			if err != nil {
				return nil, err
			}
			idpManager.providers[idp.Id] = provider
		} else if idp.Protocol == AuthProtocolOIDC {
			if _, exists := idpManager.providers[idp.Id]; exists {
				log.Debugf("OIDC provider with id %s already exists", idp.Id)
				continue
			}
			provider, err := idpManager.createOIDCProvider(idp, allowedHosts)
			if err != nil {
				return nil, err
			}
			idpManager.providers[idp.Id] = provider
		}

	}
	return &idpManager, nil
}

type idpManagerImpl struct {
	config    AuthConfig
	providers map[string]Provider
	factory   ProviderFactory
}

func (i *idpManagerImpl) GetAuthConfig() AuthConfig {
	return i.config
}

func (i *idpManagerImpl) GetProvider(id string) (Provider, bool) {
	instance, exists := i.providers[id]
	return instance, exists
}

func (i *idpManagerImpl) createSAMLProvider(idpConfig IDP, allowedHosts []string) (Provider, error) {
	samlInstance, err := CreateSAMLInstance(idpConfig.Id, idpConfig.SAMLConfiguration)
	if err != nil {
		return nil, err
	}
	return i.factory.NewSAMLProvider(samlInstance, idpConfig, allowedHosts), nil
}

func (i *idpManagerImpl) createOIDCProvider(idpConfig IDP, allowedHosts []string) (Provider, error) {
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

	oidcConfig := oauth2.Config{
		ClientID:     idpConfig.OIDCConfiguration.ClientID,
		ClientSecret: idpConfig.OIDCConfiguration.ClientSecret,
		RedirectURL:  idpConfig.OIDCConfiguration.RootURL + idpConfig.OIDCConfiguration.RedirectPath,
		Endpoint:     provider.Endpoint(),
		Scopes:       idpConfig.OIDCConfiguration.Scopes,
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: idpConfig.OIDCConfiguration.ClientID})

	return i.factory.NewOIDCProvider(idpConfig, provider, verifier, oidcConfig, allowedHosts), nil
}

func CreateSAMLInstance(idpId string, samlConfig *SAMLConfiguration) (*samlsp.Middleware, error) {
	if samlConfig == nil {
		log.Error("SAML configuration is invalid")
		return nil, fmt.Errorf("SAML configuration is invalid")
	}
	var err error
	crt, err := os.CreateTemp("", "apihub.cert")
	if err != nil {
		log.Errorf("Apihub.cert temp file wasn't created. Error - %s", err.Error())
		return nil, err
	}
	decodeSamlCert, err := base64.StdEncoding.DecodeString(samlConfig.Certificate)
	if err != nil {
		return nil, err
	}

	_, err = crt.WriteString(string(decodeSamlCert))

	if err != nil {
		log.Errorf("SAML_CRT error - %s", err)
		return nil, err
	}

	key, err := os.CreateTemp("", "apihub.key")
	if err != nil {
		log.Errorf("Apihub.key temp file wasn't created. Error - %s", err.Error())
		return nil, err
	}
	decodePrivateKey, err := base64.StdEncoding.DecodeString(samlConfig.PrivateKey)
	if err != nil {
		return nil, err
	}

	_, err = key.WriteString(string(decodePrivateKey))

	if err != nil {
		log.Errorf("SAML_KEY error - %s", err)
		return nil, err
	}

	defer key.Close()
	defer crt.Close()
	defer os.Remove(key.Name())
	defer os.Remove(crt.Name())

	keyPair, err := tls.LoadX509KeyPair(crt.Name(), key.Name())
	if err != nil {
		log.Errorf("keyPair error - %s", err)
		return nil, err
	}

	keyPair.Leaf, err = x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		log.Errorf("keyPair.Leaf error - %s", err)
		return nil, err
	}
	metadataUrl := samlConfig.IDPMetadataURL
	if metadataUrl == "" {
		log.Error("metadataUrl env is empty")
		return nil, err
	}
	idpMetadataURL, err := url.Parse(metadataUrl)
	if err != nil {
		log.Errorf("idpMetadataURL error - %s", err)
		return nil, err
	}

	tr := http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	cl := http.Client{Transport: &tr, Timeout: time.Second * 60}
	idpMetadata, err := samlsp.FetchMetadata(context.Background(), &cl, *idpMetadataURL)

	if err != nil {
		log.Errorf("idpMetadata error - %s", err)
		return nil, err
	}
	rootURLPath := samlConfig.RootURL
	if rootURLPath == "" {
		log.Error("rootURLPath env is empty")
		return nil, fmt.Errorf("rootURLPath env is empty")
	}
	rootURL, err := url.Parse(rootURLPath)
	if err != nil {
		log.Errorf("rootURL error - %s", err)
		return nil, err
	}

	samlSP, err := samlsp.New(samlsp.Options{
		URL:         *rootURL,
		Key:         keyPair.PrivateKey.(*rsa.PrivateKey),
		Certificate: keyPair.Leaf,
		IDPMetadata: idpMetadata,
		EntityID:    rootURL.Path,
	})
	if err != nil {
		log.Errorf("New saml instanse wasn't created. Error -%s", err.Error())
		return nil, err
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
