package main

// #define ERROR_LEN 250 /* for strncpy error message */
// #include <string.h> /* for strncpy error message */
import "C"
import (
	"encoding/json"
	"github.com/gwos/tng/nats"
	"github.com/gwos/tng/transit"
	"log"
)

var transitConfig transit.Transit

func main() {
}

func init() {
	_, err := nats.StartServer()
	if err != nil {
		log.Fatal(err)
	}

	err = nats.StartSubscriber(&transitConfig)
	if err != nil {
		log.Fatal(err)
	}
}

//export TestMonitoredResource
func TestMonitoredResource(str *C.char, errorMsg *C.char) *C.char {
	resource := transit.MonitoredResource{}
	if err := json.Unmarshal([]byte(C.GoString(str)), &resource); err != nil {
		C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
		return nil
	}

	// resource.Labels = map[string]string{"key1": "value1", "key02": "value02"}
	resource.Status = transit.SERVICE_PENDING
	buf, _ := json.Marshal(&resource)

	log.Printf("#TestMonitoredResource: %v, %s", resource, buf)

	/* https://github.com/golang/go/wiki/cgo#go-strings-and-c-strings */
	return C.CString(string(buf))
}

//export SendResourcesWithMetrics
func SendResourcesWithMetrics(resourcesWithMetricsJson, errorMsg *C.char) bool {
	//var resourceWithMetrics transit.SendMetricsRequest

	//err := json.Unmarshal([]byte(C.GoString(resourcesWithMetricsJson)), &resourceWithMetrics)
	//if err != nil {
	//	C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
	//	return nil
	//}

	//operationResults, err := transitConfig.SendResourcesWithMetrics(&resourceWithMetrics)
	//if err != nil {
	//	C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
	//	return nil
	//}

	err := nats.Publish(C.GoString(resourcesWithMetricsJson), nats.SendResourceWithMetricsSubject)
	if err != nil {
		C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
		return false
	}

	return true

	//operationResultsJson, err := json.Marshal(operationResults)
	//if err != nil {
	//	C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
	//	return nil
	//}
	//return C.CString(string(operationResultsJson))
}

//export ListMetrics
func ListMetrics(errorMsg *C.char) *C.char {
	monitorDescriptor, err := transitConfig.ListMetrics()
	if err != nil {
		C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
		return nil
	}

	bytes, err := json.Marshal(monitorDescriptor)
	if err != nil {
		C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
		return nil
	}

	return C.CString(string(bytes))
}

//export SynchronizeInventory
func SynchronizeInventory(inventoryJson, errorMsg *C.char) bool {
	//fmt.Println(C.GoString(inventoryJson))

	//var inventory transit.TransitSendInventoryRequest
	//
	//err := json.Unmarshal([]byte(C.GoString(inventoryJson)), &inventory)
	//if err != nil {
	//	C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
	//	return nil
	//}

	err := nats.Publish(C.GoString(inventoryJson), nats.SynchronizeInventorySubject)
	if err != nil {
		C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
		return false
	}

	return true

	//if err != nil {
	//	C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
	//	return nil
	//}
	//
	//operationResultsJson, err := json.Marshal(operationResults)
	//if err != nil {
	//	C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
	//	return nil
	//}
	//
	//return C.CString(string(operationResultsJson))
}

//TODO:
func ListInventory() {
}

//export Connect
func Connect(credentialsJson, errorMsg *C.char) bool {
	transitConfig = transit.Transit{}

	var credentials transit.Credentials

	err := json.Unmarshal([]byte(C.GoString(credentialsJson)), &credentials)
	if err != nil {
		C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
		return false
	}

	err = transitConfig.Connect(credentials)
	if err != nil {
		C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
		return false
	}

	//start nats

	return true
}

//export Disconnect
func Disconnect(errorMsg *C.char) bool {
	err := transitConfig.Disconnect()
	if err != nil {
		C.strncpy((*C.char)(errorMsg), C.CString(err.Error()), C.ERROR_LEN)
		return false
	}
	return true
}
