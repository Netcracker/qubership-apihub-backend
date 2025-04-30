package security

import (
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security/idp"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/crewjam/saml/samlsp"
	"golang.org/x/oauth2"
)

type ProviderFactoryImpl struct{}

func NewProviderFactory() idp.ProviderFactory {
	return &ProviderFactoryImpl{}
}

func (f *ProviderFactoryImpl) NewSAMLProvider(samlInstance *samlsp.Middleware, config idp.IDP, allowedHosts []string) idp.Provider {
	return newSAMLProvider(samlInstance, config, allowedHosts)
}

func (f *ProviderFactoryImpl) NewOIDCProvider(config idp.IDP, provider *oidc.Provider, verifier *oidc.IDTokenVerifier, oAuth2Config oauth2.Config, allowedHosts []string) idp.Provider {
	return newOIDCProvider(config, provider, verifier, oAuth2Config, allowedHosts)
}
