package clients

import (
	"context"
	"github.com/rs/zerolog/log"
	"sync"
)

// TCGConnection defines TCG Connection configuration
type TCGConnection struct {
	ID int `yaml:"id"`
	// HostName accepts value for combined "host:port"
	// used as `url.URL{HostName}`
	HostName            string `yaml:"hostName"`
	UserName            string `yaml:"userName"`
	Password            string `yaml:"password"`
	Enabled             bool   `yaml:"enabled"`
	IsChild             bool   `yaml:"isChild"`
	DisplayName         string `yaml:"displayName"`
	MergeHosts          bool   `yaml:"mergeHosts"`
	LocalConnection     bool   `yaml:"localConnection"`
	DeferOwnership      string `yaml:"deferOwnership"`
	PrefixResourceNames bool   `yaml:"prefixResourceNames"`
	ResourceNamePrefix  string `yaml:"resourceNamePrefix"`
	SendAllInventory    bool   `yaml:"sendAllInventory"`
	IsDynamicInventory  bool   `yaml:"-"`
	HTTPEncode          bool   `yaml:"-"`
}

// TCGHostGroups defines collection
type TCGHostGroups struct {
	HostGroups []struct {
		Name  string `json:"name"`
		Hosts []struct {
			HostName string `json:"hostName"`
		} `json:"hosts"`
	} `json:"hostGroups"`
}

// TCGServices defines collection
type TCGServices struct {
	Services []struct {
		HostName string `json:"hostName"`
	} `json:"services"`
}

// TCGClient implements TCG Connection
type TCGClient struct {
	AppName string
	AppType string
	*TCGConnection

	mu   sync.Mutex
	once sync.Once
}

func (client *TCGClient) SendData(ctx context.Context, payload []byte) ([]byte, error) {
	log.Info().Msgf("Sending Metrics Data: %s", payload)
	return nil, nil
}

func (client *TCGClient) SendEvent(ctx context.Context, payload []byte) ([]byte, error) {
	log.Info().Msgf("Sending Event: %s", payload)
	return nil, nil
}

// Connect calls API
func (client *TCGClient) Connect() error {
	return nil
}

// Disconnect calls API
func (client *TCGClient) Disconnect() error {
	return nil
}
