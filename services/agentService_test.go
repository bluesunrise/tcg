package services

import (
	"github.com/gwos/tng/config"
	"github.com/gwos/tng/log"
	"github.com/stretchr/testify/assert"
	"testing"
)

func init() {
	log.Error("HEAREEEE")
	config.GetConfig().Connector.NatsStoreType = "MEMORY"
	config.GetConfig().GWConnections = []*config.GWConnection{
		{
			HostName: "test",
			UserName: "test",
			Password: "test",
			Enabled: true,
		},
	}
}

func TestAgentService_StartStopNats(t *testing.T) {
	assert.NoError(t, GetAgentService().StartNats())
	assert.NoError(t, GetAgentService().StopNats())
}

func TestAgentService_StartStopController(t *testing.T) {
	assert.NoError(t, GetAgentService().StartController())
	assert.NoError(t, GetAgentService().StopController())
}

func TestAgentService_StartStopTransport(t *testing.T) {
	assert.NoError(t, GetAgentService().StartNats())
	assert.NoError(t, GetAgentService().StartTransport())
	assert.NoError(t, GetAgentService().StopTransport())
	assert.NoError(t, GetAgentService().StartTransport())
	assert.NoError(t, GetAgentService().StopNats())
}
