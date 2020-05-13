package main

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/gwos/tcg/connectors"
	"github.com/gwos/tcg/connectors/elastic-connector/model"
	_ "github.com/gwos/tcg/docs"
	"github.com/gwos/tcg/log"
	"github.com/gwos/tcg/services"
	"net/http"
	"time"
)

func main() {
	connectors.ControlCHandler()

	var transitService = services.GetTransitService()

	connector := ElasticConnector{}

	var config *model.ElasticConnectorConfig
	var configMark []byte

	transitService.ConfigHandler = func(data []byte) {
		log.Info("[Elastic Connector]: Configuration received")
		connection, profile, gwConnections := connectors.RetrieveCommonConnectorInfo(data)
		cfg := model.InitConfig(transitService.Connector.AppType, transitService.Connector.AgentID,
			&connection, &profile, &gwConnections)
		config = cfg
		connectors.Timer = config.Timer
		cfgMark, _ := json.Marshal(cfg)
		if !bytes.Equal(configMark, cfgMark) {
			if err := connector.LoadConfig(config); err != nil {
				log.Error("Cannot reload ElasticConnector config: ", err)
			} else {
				configMark = cfgMark
				connector.performCollection()
			}
		}
	}

	if err := transitService.DemandConfig(
		services.Entrypoint{
			Url:    "/suggest/:viewName/:name",
			Method: "Get",
			Handler: func(c *gin.Context) {
				c.JSON(http.StatusOK, connector.ListSuggestions(c.Param("viewName"), c.Param("name")))
			},
		},
	); err != nil {
		log.Error(err)
		return
	}

	if err := connectors.Start(); err != nil {
		log.Error(err)
		return
	}

	for {
		connector.performCollection()
		time.Sleep(time.Duration(int64(connector.config.Timer) * int64(time.Second)))
	}
}
