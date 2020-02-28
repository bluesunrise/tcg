package main

import (
	"github.com/gwos/tng/connectors"
	"github.com/gwos/tng/log"
)

func main() {
	monitoredResources, inventoryResources, resourceGroups, err := CollectMetrics()
	if err != nil {
		log.Error(err.Error())
	}
	connectors.ControlCHandler()
	err = connectors.Start()
	if err != nil {
		log.Error(err.Error())
		return
	}
	err = connectors.SendInventory(inventoryResources, resourceGroups)
	if err != nil {
		log.Error(err.Error())
	}
	err = connectors.SendMetrics(monitoredResources)
	if err != nil {
		log.Error(err.Error())
	}
}
