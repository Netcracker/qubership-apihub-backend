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

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	log "github.com/sirupsen/logrus"
)

// NewOpenAIClient creates a new OpenAI client configured for OpenAI API requests
// with proxy support, insecure skip verify, extended timeout and API key
func NewOpenAIClient(apiKey, proxyURL string) (openai.Client, error) {
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

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   20 * time.Minute, // 20 minutes timeout
	}

	// Create OpenAI client with custom HTTP client
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
	}

	client := openai.NewClient(opts...)

	return client, nil
}
