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

package midldleware

import (
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/metrics"
	"github.com/gorilla/mux"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{w, http.StatusOK}
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func PrometheusMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := mux.CurrentRoute(r)
		path, _ := route.GetPathTemplate()
		statusCode := 200
		now := time.Now()

		if strings.Contains(path, "/ws/") {
			next.ServeHTTP(w, r)
		} else {
			lrw := newLoggingResponseWriter(w)
			next.ServeHTTP(lrw, r)
			statusCode = lrw.statusCode
		}

		elapsedSeconds := time.Since(now).Seconds()

		metrics.TotalRequests.WithLabelValues(path, strconv.Itoa(statusCode), r.Method).Inc()
		metrics.HttpDuration.WithLabelValues(path, strconv.Itoa(statusCode), r.Method).Observe(elapsedSeconds)
	})
}
