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

package service

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
)

type LogsService interface {
	StoreLogs(obj map[string]interface{})
}

func NewLogsService() LogsService {
	return &logsServiceImpl{}
}

type logsServiceImpl struct {
}

func (l logsServiceImpl) StoreLogs(obj map[string]interface{}) {
	fields := make([]string, 0)
	for key, value := range obj {
		fields = append(fields, fmt.Sprintf("%v: %v", key, value))
	}
	log.Error(strings.Join(fields, ", ")) //todo maybe log.Info?
}
