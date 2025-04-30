package idp

import (
	"net/http"
)

type Provider interface {
	GetId() string
	StartAuthentication(w http.ResponseWriter, r *http.Request, allowedHosts []string)
	CallbackHandler(w http.ResponseWriter, r *http.Request)
	ServeMetadata(w http.ResponseWriter, r *http.Request)
}
