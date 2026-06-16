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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

const customCACertsPathEnv = "CUSTOM_CA_CERTS_PATH"

var (
	baseTLSOnce sync.Once
	baseTLSCfg  *tls.Config
	baseTLSErr  error
)

// ValidateTLSAtStartup validates the default TLS configuration at process startup.
func ValidateTLSAtStartup() error {
	_, err := BuildSecureTLSConfig(nil)
	return err
}

// BuildSecureTLSConfig returns a TLS configuration with proper certificate validation.
// It uses the system certificate pool, optional inline PEM data, and custom CA certificates
// from paths specified in the CUSTOM_CA_CERTS_PATH environment variable.
// Multiple paths can be separated by colons (:) on Unix or semicolons (;) on Windows.
func BuildSecureTLSConfig(customPEM []byte) (*tls.Config, error) {
	if len(customPEM) == 0 {
		baseTLSOnce.Do(func() {
			baseTLSCfg, baseTLSErr = buildSecureTLSConfig(nil)
		})
		if baseTLSErr != nil {
			return nil, baseTLSErr
		}
		return baseTLSCfg.Clone(), nil
	}
	return buildSecureTLSConfig(customPEM)
}

func buildSecureTLSConfig(customPEM []byte) (*tls.Config, error) {
	rootCAs, err := buildRootCertPool(customPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		RootCAs:    rootCAs,
		MinVersion: tls.VersionTLS12,
	}, nil
}

func buildRootCertPool(customPEM []byte) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system certificate pool: %w", err)
	}
	if len(customPEM) > 0 {
		if ok := pool.AppendCertsFromPEM(customPEM); !ok {
			return nil, fmt.Errorf("parse custom PEM certificate")
		}
	}
	if err := loadCustomCACertsFromEnv(pool); err != nil {
		return nil, err
	}
	return pool, nil
}

func loadCustomCACertsFromEnv(pool *x509.CertPool) error {
	customCAPath := os.Getenv(customCACertsPathEnv)
	if customCAPath == "" {
		return nil
	}

	separator := ":"
	if os.PathSeparator == '\\' {
		separator = ";"
	}

	paths := strings.Split(customCAPath, separator)
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if err := loadCertsFromPath(pool, path); err != nil {
			return err
		}
	}
	return nil
}

func loadCertsFromPath(pool *x509.CertPool, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("access custom CA certificate path %s: %w", path, err)
	}

	if info.IsDir() {
		loaded, err := loadCertsFromDirectory(pool, path)
		if err != nil {
			return err
		}
		if !loaded {
			return fmt.Errorf("no certificates found in custom CA directory %s", path)
		}
		return nil
	}
	return loadCertFromFile(pool, path)
}

func loadCertsFromDirectory(pool *x509.CertPool, dirPath string) (bool, error) {
	loaded := false
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".crt" || ext == ".pem" {
				if err := loadCertFromFile(pool, path); err != nil {
					return err
				}
				loaded = true
			}
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("walk directory %s for certificates: %w", dirPath, err)
	}
	return loaded, nil
}

func loadCertFromFile(pool *x509.CertPool, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read certificate file %s: %w", filePath, err)
	}
	if ok := pool.AppendCertsFromPEM(data); !ok {
		return fmt.Errorf("parse certificate from file %s", filePath)
	}
	log.Infof("Successfully loaded custom CA certificate from %s", filePath)
	return nil
}
