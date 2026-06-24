package utils

import (
	"fmt"
	"strings"

	mcputil "github.com/mark3labs/mcp-go/util"
	log "github.com/sirupsen/logrus"
)

const mcpSweepingExpiredSessionLogPrefix = "Sweeping expired session:"

type mcpGoLogger struct{}

func (mcpGoLogger) Infof(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	if strings.HasPrefix(msg, mcpSweepingExpiredSessionLogPrefix) {
		log.Trace(msg)
		return
	}
	log.Info(msg)
}

func (mcpGoLogger) Errorf(format string, v ...any) {
	log.Errorf(format, v...)
}

func NewMCPGoLogger() mcputil.Logger {
	return mcpGoLogger{}
}
