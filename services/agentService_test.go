package services

import (
	"github.com/gwos/tng/setup"
	"github.com/stretchr/testify/assert"
	"testing"
)

func init() {
	setup.GetConfig().Connector.NatsStoreType = "MEMORY"
	setup.GetConfig().GWConnections = []*setup.GWConnection{
		{
			HostName: "test",
			UserName: "test",
			Password: "test",
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
