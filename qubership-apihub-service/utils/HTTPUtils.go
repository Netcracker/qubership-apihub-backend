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

package utils

import (
	"encoding/json"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	log "github.com/sirupsen/logrus"
	"net/http"
)

func RedirectHandler(w http.ResponseWriter, r *http.Request) {
	redirectURI := r.URL.Query().Get("redirectUri")
	if redirectURI == "" {
		redirectURI = "/"
	}
	log.Debugf("redirect url - %s", redirectURI)

	http.Redirect(w, r, redirectURI, http.StatusFound)
}

func RespondWithError(w http.ResponseWriter, msg string, err error) {
	log.Errorf("%s: %s", msg, err.Error())
	if customError, ok := err.(*exception.CustomError); ok {
		RespondWithCustomError(w, customError)
	} else {
		RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusInternalServerError,
			Message: msg,
			Debug:   err.Error()})
	}
}

func RespondWithCustomError(w http.ResponseWriter, err *exception.CustomError) {
	log.Debugf("Request failed. Code = %d. Message = %s. Params: %v. Debug: %s", err.Status, err.Message, err.Params, err.Debug)
	RespondWithJson(w, err.Status, err)
}

func RespondWithJson(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
