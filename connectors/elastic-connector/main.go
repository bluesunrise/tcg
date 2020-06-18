package main

import (
	"bytes"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gwos/tcg/config"
	"github.com/gwos/tcg/connectors"
	_ "github.com/gwos/tcg/docs"
	"github.com/gwos/tcg/log"
	"github.com/gwos/tcg/services"
	"net/http"
	"time"
)

// Variables to control connector version and build time.
// Can be overridden during the build step.
// See README for details.
var (
	buildTime = "Build time not provided"
	buildTag  = "8.1.0"
)

func main() {
	connectors.ControlCHandler()

	var transitService = services.GetTransitService()

	log.Info(fmt.Sprintf("[Elastic Connector]: BuildVersion: %s   /   Build time: %s", buildTag, buildTime))

	var connector ElasticConnector

	var cfg ElasticConnectorConfig
	var cfgChksum, iChksum []byte
	transitService.ConfigHandler = func(data []byte) {
		log.Info("[Elastic Connector]: Configuration received")
		if monitorConn, profile, gwConnections, err := connectors.RetrieveCommonConnectorInfo(data); err == nil {
			c := InitConfig(config.GetConfig().Connector.AppType, config.GetConfig().Connector.AgentID,
				monitorConn, profile, gwConnections)
			cfg = *c
			connectors.Timer = cfg.Timer
			chk, err := connectors.Hashsum(
				config.GetConfig().GWConnections,
				cfg,
			)
			if err != nil || !bytes.Equal(cfgChksum, chk) {
				if err := connector.LoadConfig(cfg); err != nil {
					log.Error("Cannot reload ElasticConnector config: ", err)
				} else {
					_, inventory, groups := connector.CollectMetrics()
					log.Info("[Elastic Connector]: Sending inventory ...")
					err := connectors.SendInventory(inventory, groups, connector.config.Ownership)
					if err != nil {
						log.Error(err.Error())
					}
					iChk, iChkErr := connector.getInventoryHashSum()
					if iChkErr == nil {
						iChksum = iChk
					}
				}
			}
			if err == nil {
				cfgChksum = chk
			}
		} else {
			log.Error("[Elastic Connector]: Error during parsing config. Aborting ...")
			return
		}
	}

	log.Info("[Elastic Connector]: Waiting for configuration to be delivered ...")
	if err := transitService.DemandConfig(
		services.Entrypoint{
			Url:    "/suggest/:viewName",
			Method: "Get",
			Handler: func(c *gin.Context) {
				c.JSON(http.StatusOK, connector.ListSuggestions(c.Param("viewName"), ""))
			},
		},
		services.Entrypoint{
			Url:    "/suggest/:viewName/:name",
			Method: "Get",
			Handler: func(c *gin.Context) {
				c.JSON(http.StatusOK, connector.ListSuggestions(c.Param("viewName"), c.Param("name")))
			},
		},
		services.Entrypoint{
			Url:    "/version",
			Method: "Get",
			Handler: func(c *gin.Context) {
				c.JSON(http.StatusOK, connectors.BuildVersion{Tag: buildTag,
					Time: buildTime})
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
		if len(connector.monitoringState.Metrics) > 0 {
			metrics, inventory, groups := connector.CollectMetrics()

			chk, chkErr := connector.getInventoryHashSum()
			if chkErr != nil || !bytes.Equal(iChksum, chk) {
				log.Info("[Elastic Connector]: Inventory changed. Sending inventory ...")
				err := connectors.SendInventory(inventory, groups, connector.config.Ownership)
				if err != nil {
					log.Error(err.Error())
				}
			}
			if chkErr == nil {
				iChksum = chk
			}

			log.Info("[Elastic Connector]: Monitoring resources ...")
			err := connectors.SendMetrics(metrics)
			if err != nil {
				log.Error(err.Error())
			}
		}
		time.Sleep(time.Duration(connectors.Timer * int64(time.Second)))
	}
}
