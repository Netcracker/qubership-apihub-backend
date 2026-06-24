package utils

import (
	"bytes"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestMCPGoLogger_SweepingExpiredSessionAtTrace(t *testing.T) {
	var buf bytes.Buffer
	prevOut := log.StandardLogger().Out
	prevLevel := log.GetLevel()
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetLevel(prevLevel)
	})

	log.SetOutput(&buf)
	log.SetLevel(log.InfoLevel)

	logger := NewMCPGoLogger()
	logger.Infof("Sweeping expired session: %s", "mcp-session-test")

	assert.Empty(t, buf.String())

	log.SetLevel(log.TraceLevel)
	logger.Infof("Sweeping expired session: %s", "mcp-session-test")

	assert.Contains(t, buf.String(), "Sweeping expired session: mcp-session-test")
}

func TestMCPGoLogger_OtherInfoMessagesStayAtInfo(t *testing.T) {
	var buf bytes.Buffer
	prevOut := log.StandardLogger().Out
	prevLevel := log.GetLevel()
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetLevel(prevLevel)
	})

	log.SetOutput(&buf)
	log.SetLevel(log.InfoLevel)

	logger := NewMCPGoLogger()
	logger.Infof("Delivered sampling response for session %s, request %d", "mcp-session-test", 1)

	assert.Contains(t, buf.String(), "Delivered sampling response for session mcp-session-test, request 1")
}
