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

package cookie

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

const SessionCookieName = "apihub-session"

type SessionCookie struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func ExtractSessionCookie(r *http.Request) (*SessionCookie, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, fmt.Errorf("session cookie not found: %w", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode session cookie value: %w", err)
	}

	var session SessionCookie
	if err := json.Unmarshal(decoded, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session cookie: %w", err)
	}

	return &session, nil
}
