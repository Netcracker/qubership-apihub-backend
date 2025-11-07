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

package client

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	log "github.com/sirupsen/logrus"
)

// NewOpenAIClient creates a new HTTP client configured for OpenAI API requests
// with proxy support, insecure skip verify, and extended timeout
func NewOpenAIClient(proxyURL string) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	// Configure proxy if provided
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			log.Warnf("Failed to parse proxy URL '%s': %v. Continuing without proxy.", proxyURL, err)
		} else {
			transport.Proxy = http.ProxyURL(proxy)
			log.Infof("OpenAI client configured with proxy: %s", proxyURL)
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   20 * time.Minute, // 20 minutes timeout
	}

	return client
}

