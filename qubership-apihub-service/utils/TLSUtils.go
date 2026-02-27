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
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

// GetSecureTLSConfig returns a TLS configuration with proper certificate validation.
// It uses the system certificate pool and optionally loads custom CA certificates
// from paths specified in the CUSTOM_CA_CERTS_PATH environment variable.
// Multiple paths can be separated by colons (:) on Unix or semicolons (;) on Windows.
func GetSecureTLSConfig() *tls.Config {
	rootCAs := getSystemCertPool()
	loadCustomCACerts(rootCAs)

	return &tls.Config{
		RootCAs:    rootCAs,
		MinVersion: tls.VersionTLS12, // Enforce TLS 1.2 minimum
	}
}

// GetSecureTLSConfigWithCustomCerts returns a TLS configuration with custom CA certificates
// added to the system certificate pool. This is useful when you need to trust specific
// certificates in addition to the system trust store.
func GetSecureTLSConfigWithCustomCerts(pemData []byte) *tls.Config {
	rootCAs := getSystemCertPool()

	if pemData != nil && len(pemData) > 0 {
		if ok := rootCAs.AppendCertsFromPEM(pemData); !ok {
			log.Warn("Failed to append custom certificate to root CA pool")
		} else {
			log.Debug("Successfully added custom certificate to root CA pool")
		}
	}

	loadCustomCACerts(rootCAs)

	return &tls.Config{
		RootCAs:    rootCAs,
		MinVersion: tls.VersionTLS12,
	}
}

// getSystemCertPool retrieves the system certificate pool.
// If the system pool cannot be loaded, it returns a new empty pool.
func getSystemCertPool() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil {
		log.Warnf("Failed to load system certificate pool, using empty pool: %v", err)
		return x509.NewCertPool()
	}
	return pool
}

// loadCustomCACerts loads custom CA certificates from paths specified in
// the CUSTOM_CA_CERTS_PATH environment variable and adds them to the provided cert pool.
// Paths can be files or directories. Directories are scanned recursively for .crt and .pem files.
func loadCustomCACerts(pool *x509.CertPool) {
	customCAPath := os.Getenv("CUSTOM_CA_CERTS_PATH")
	if customCAPath == "" {
		return
	}

	// Support multiple paths separated by : (Unix) or ; (Windows)
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

		loadCertsFromPath(pool, path)
	}
}

// loadCertsFromPath loads certificates from a file or directory path
func loadCertsFromPath(pool *x509.CertPool, path string) {
	info, err := os.Stat(path)
	if err != nil {
		log.Warnf("Failed to access custom CA certificate path %s: %v", path, err)
		return
	}

	if info.IsDir() {
		loadCertsFromDirectory(pool, path)
	} else {
		loadCertFromFile(pool, path)
	}
}

// loadCertsFromDirectory recursively loads all .crt and .pem files from a directory
func loadCertsFromDirectory(pool *x509.CertPool, dirPath string) {
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".crt" || ext == ".pem" {
				loadCertFromFile(pool, path)
			}
		}
		return nil
	})

	if err != nil {
		log.Warnf("Failed to walk directory %s for certificates: %v", dirPath, err)
	}
}

// loadCertFromFile loads a certificate from a file and adds it to the pool
func loadCertFromFile(pool *x509.CertPool, filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Warnf("Failed to read certificate file %s: %v", filePath, err)
		return
	}

	if ok := pool.AppendCertsFromPEM(data); !ok {
		log.Warnf("Failed to parse certificate from file %s", filePath)
	} else {
		log.Infof("Successfully loaded custom CA certificate from %s", filePath)
	}
}
