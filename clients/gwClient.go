package clients

import (
	"encoding/json"
	"fmt"
	"github.com/gwos/tng/config"
	"github.com/gwos/tng/log"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// GWOperations defines Groundwork operations interface
type GWOperations interface {
	Connect() error
	Disconnect() error
	ValidateToken(appName, apiToken string) error
	SendEvents(payload []byte) ([]byte, error)
	SendEventsAck(payload []byte) ([]byte, error)
	SendEventsUnack(payload []byte) ([]byte, error)
	SendResourcesWithMetrics(payload []byte) ([]byte, error)
	SynchronizeInventory(payload []byte) ([]byte, error)
}

// Define entrypoints for GWOperations
const (
	GWLocalEntryPointConstant           = "http://foundation:8080/api"
	GWEntrypointConnectRemote           = "/api/users/authenticatePassword"
	GWEntrypointConnect                 = "/api/auth/login"
	GWEntrypointDisconnect              = "/api/auth/logout"
	GWEntrypointSendEvents              = "/api/events"
	GWEntrypointSendEventsAck           = "/api/events/ack"
	GWEntrypointSendEventsUnack         = "/api/events/unack"
	GWEntrypointSendResourceWithMetrics = "/api/monitoring"
	GWEntrypointSynchronizeInventory    = "/api/synchronizer"
	GWEntrypointValidateToken           = "/api/auth/validatetoken"
)

// GWClient implements GWOperations interface
type GWClient struct {
	AppName string
	*config.GWConnection
	sync.Mutex
	token string
	sync.Once
	uriConnect                 string
	uriDisconnect              string
	uriSendEvents              string
	uriSendEventsAck           string
	uriSendEventsUnack         string
	uriSendResourceWithMetrics string
	uriSynchronizeInventory    string
	uriValidateToken           string
}

type AuthPayload struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type UserResponse struct {
	Name        string `json:"name"`
	AccessToken string `json:accessToken`
}

// Connect implements GWOperations.Connect.
func (client *GWClient) Connect() error {
	client.buildURIs()
	prevToken := client.token
	/* restrict by mutex for one-thread at one-time */
	client.Lock()
	defer client.Unlock()
	if prevToken != client.token {
		/* token already changed */
		return nil
	}

	// TODO: find a better way to determine if local or remote connection than this:
	if client.GWConnection.HostName == GWLocalEntryPointConstant {
		formValues := map[string]string{
			"gwos-app-name": client.AppName,
			"user":          client.GWConnection.UserName,
			"password":      client.GWConnection.Password,
		}
		headers := map[string]string{
			"Accept":       "text/plain",
			"Content-Type": "application/x-www-form-urlencoded",
		}
		reqURL := client.uriConnect
		statusCode, byteResponse, err := SendRequest(http.MethodPost, reqURL, headers, formValues, nil)
		logEntry := log.With(log.Fields{
			"error":      err,
			"response":   string(byteResponse),
			"statusCode": statusCode,
		}).WithDebug(log.Fields{
			"headers":   headers,
			"reqURL 01": reqURL,
		})
		logEntryLevel := log.InfoLevel
		defer func() {
			logEntry.Log(logEntryLevel, "GWClient: connect")
		}()

		if err != nil {
			logEntryLevel = log.ErrorLevel
			return err
		}
		if statusCode == 200 {
			client.token = string(byteResponse)
			logEntry.WithDebug(log.Fields{
				"token": client.token,
			})
			return nil
		}
		return fmt.Errorf(string(byteResponse))
	} else {
		authPayload := AuthPayload{
			Name:     client.GWConnection.UserName,
			Password: client.GWConnection.Password,
		}
		authBytes, err := json.Marshal(authPayload)
		headers := map[string]string{
			"Accept":        "application/json",
			"Content-Type":  "application/json",
			"GWOS-APP-NAME": client.AppName,
		}
		reqURL := client.uriConnect
		statusCode, byteResponse, err := SendRequest(http.MethodPut, reqURL, headers, nil, authBytes)
		log.Info("*** reqURL2: ", reqURL, statusCode, string(byteResponse), err)
		logEntry := log.With(log.Fields{
			"error":      err,
			"response":   string(byteResponse),
			"statusCode": statusCode,
		}).WithDebug(log.Fields{
			"headers":   headers,
			"reqURL 01": reqURL,
		})
		logEntryLevel := log.InfoLevel
		defer func() {
			logEntry.Log(logEntryLevel, "GWClient: connect")
		}()

		if err != nil {
			logEntryLevel = log.ErrorLevel
			return err
		}
		user := UserResponse{AccessToken: ""}
		error2 := "unknown error"
		if statusCode == 200 {
			// client.token = string(byteResponse)
			error2 := json.Unmarshal(byteResponse, &user)
			if error2 == nil {
				client.token = user.AccessToken
				logEntry.WithDebug(log.Fields{
					"user": user,
				})
				return nil
			}
		}
		logEntry.WithInfo(log.Fields{
			"errorCode": error2,
		})
		return fmt.Errorf(string(error2))
	}
}

// Disconnect implements GWOperations.Disconnect.
func (client *GWClient) Disconnect() error {
	client.buildURIs()
	formValues := map[string]string{
		"gwos-app-name":  client.AppName,
		"gwos-api-token": client.token,
	}
	headers := map[string]string{
		"Accept":       "text/plain",
		"Content-Type": "application/x-www-form-urlencoded",
	}
	reqURL := client.uriDisconnect
	statusCode, byteResponse, err := SendRequest(http.MethodPost, reqURL, headers, formValues, nil)

	logEntry := log.With(log.Fields{
		"error":      err,
		"response":   string(byteResponse),
		"statusCode": statusCode,
	}).WithDebug(log.Fields{
		"headers": headers,
		"reqURL":  reqURL,
	})
	logEntryLevel := log.InfoLevel
	defer func() {
		logEntry.Log(logEntryLevel, "GWClient: disconnect")
	}()

	if err != nil {
		return err
	}

	if statusCode == 200 {
		return nil
	}
	return fmt.Errorf(string(byteResponse))
}

// ValidateToken implements GWOperations.ValidateToken.
func (client *GWClient) ValidateToken(appName, apiToken string) error {
	client.buildURIs()
	headers := map[string]string{
		"Accept":       "text/plain",
		"Content-Type": "application/x-www-form-urlencoded",
	}
	formValues := map[string]string{
		"gwos-app-name":  appName,
		"gwos-api-token": apiToken,
	}
	reqURL := client.uriValidateToken
	statusCode, byteResponse, err := SendRequest(http.MethodPost, reqURL, headers, formValues, nil)

	logEntry := log.With(log.Fields{
		"error":      err,
		"response":   string(byteResponse),
		"statusCode": statusCode,
	}).WithDebug(log.Fields{
		"headers": headers,
		"reqURL":  reqURL,
	})
	logEntryLevel := log.InfoLevel
	defer func() {
		logEntry.Log(logEntryLevel, "GWClient: validate token")
	}()

	if err == nil {
		if statusCode == 200 {
			b, _ := strconv.ParseBool(string(byteResponse))
			if b {
				return nil
			}
			return fmt.Errorf("invalid gwos-app-name or gwos-api-token")
		}
		return fmt.Errorf(string(byteResponse))
	}

	return err
}

// SynchronizeInventory implements GWOperations.SynchronizeInventory.
func (client *GWClient) SynchronizeInventory(payload []byte) ([]byte, error) {
	client.buildURIs()
	return client.sendData(client.uriSynchronizeInventory, payload)
}

// SendResourcesWithMetrics implements GWOperations.SendResourcesWithMetrics.
func (client *GWClient) SendResourcesWithMetrics(payload []byte) ([]byte, error) {
	client.buildURIs()
	return client.sendData(client.uriSendResourceWithMetrics, payload)
}

// SendEvents implements GWOperations.SendEvents.
func (client *GWClient) SendEvents(payload []byte) ([]byte, error) {
	client.buildURIs()
	return client.sendData(client.uriSendEvents, payload)
}

// SendEventsAck implements GWOperations.SendEventsAck.
func (client *GWClient) SendEventsAck(payload []byte) ([]byte, error) {
	client.buildURIs()
	return client.sendData(client.uriSendEventsAck, payload)
}

// SendEventsUnack implements GWOperations.SendEventsUnack.
func (client *GWClient) SendEventsUnack(payload []byte) ([]byte, error) {
	client.buildURIs()
	return client.sendData(client.uriSendEventsUnack, payload)
}

func (client *GWClient) sendData(reqURL string, payload []byte) ([]byte, error) {
	headers := map[string]string{
		"Accept":         "application/json",
		"Content-Type":   "application/json",
		"GWOS-APP-NAME":  client.AppName,
		"GWOS-API-TOKEN": client.token,
	}
	statusCode, byteResponse, err := SendRequest(http.MethodPost, reqURL, headers, nil, payload)
	log.Info("*** sendData: ", reqURL, client.token, statusCode, err)
	if statusCode == 401 {
		if err := client.Connect(); err != nil {
			return nil, err
		}
		headers["GWOS-API-TOKEN"] = client.token
		statusCode, byteResponse, err = SendRequest(http.MethodPost, reqURL, headers, nil, payload)
	}

	logEntry := log.With(log.Fields{
		"error":      err,
		"response":   string(byteResponse),
		"statusCode": statusCode,
	}).WithDebug(log.Fields{
		"headers": headers,
		"payload": string(payload),
		"reqURL":  reqURL,
	})
	logEntryLevel := log.InfoLevel
	defer func() {
		logEntry.Log(logEntryLevel, "GWClient: sendData")
	}()

	if err != nil {
		logEntryLevel = log.ErrorLevel
		return nil, err
	}
	if statusCode != 200 {
		logEntryLevel = log.WarnLevel
		return nil, fmt.Errorf(string(byteResponse))
	}
	return byteResponse, nil
}

func (client *GWClient) buildURIs() {
	client.Once.Do(func() {
		uriConnect := buildURI(client.GWConnection.HostName, GWEntrypointConnect)
		// TODO: find a better way to determine if local or remote connection than this:
		if client.GWConnection.HostName != GWLocalEntryPointConstant {
			uriConnect = buildURI(client.GWConnection.HostName, GWEntrypointConnectRemote)
		}
		uriDisconnect := buildURI(client.GWConnection.HostName, GWEntrypointDisconnect)
		uriSendEvents := buildURI(client.GWConnection.HostName, GWEntrypointSendEvents)
		uriSendEventsAck := buildURI(client.GWConnection.HostName, GWEntrypointSendEventsAck)
		uriSendEventsUnack := buildURI(client.GWConnection.HostName, GWEntrypointSendEventsUnack)
		uriSendResourceWithMetrics := buildURI(client.GWConnection.HostName, GWEntrypointSendResourceWithMetrics)
		uriSynchronizeInventory := buildURI(client.GWConnection.HostName, GWEntrypointSynchronizeInventory)
		uriValidateToken := buildURI(client.GWConnection.HostName, GWEntrypointValidateToken)
		client.Mutex.Lock()
		client.uriConnect = uriConnect
		client.uriDisconnect = uriDisconnect
		client.uriSendEvents = uriSendEvents
		client.uriSendEventsAck = uriSendEventsAck
		client.uriSendEventsUnack = uriSendEventsUnack
		client.uriSendResourceWithMetrics = uriSendResourceWithMetrics
		client.uriSynchronizeInventory = uriSynchronizeInventory
		client.uriValidateToken = uriValidateToken
		client.Mutex.Unlock()
	})
}

func buildURI(hostname, entrypoint string) string {
	s := strings.TrimSuffix(strings.TrimRight(hostname, "/"), "/api")
	if !strings.HasPrefix(s, "http") {
		s = "https://" + s
	}
	return fmt.Sprintf("%s/%s", s, strings.TrimLeft(entrypoint, "/"))
}
