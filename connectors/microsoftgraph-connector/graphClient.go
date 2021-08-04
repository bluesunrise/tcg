package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PaesslerAG/jsonpath"
	"github.com/gwos/tcg/connectors"
	"github.com/gwos/tcg/log"
	"github.com/gwos/tcg/transit"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
)

var httpClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

func login(tenantID string, clientID string, clientSecret string, resource string) (str string, err error) {
	var (
		responseBody []byte
		body         io.Reader
		token        interface{}
		v            interface{}
		request      *http.Request
		response     *http.Response
	)

	endPoint := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/token", tenantID)

	auth := AuthRecord{
		GrantType:    "client_credentials",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Resource:     resource,
	}

	form := url.Values{
		"grant_type":    []string{"client_credentials"},
		"client_secret": []string{auth.ClientSecret},
		"client_id":     []string{auth.ClientID},
		"resource":      []string{auth.Resource},
	}

	body = bytes.NewBuffer([]byte(form.Encode()))
	if request, err = http.NewRequest(http.MethodPost, endPoint, body); err == nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if response, err = Do(request); err != nil {
			return
		}
	} else {
		return
	}

	defer func() {
		_ = response.Body.Close()
	}()

	if responseBody, err = ioutil.ReadAll(response.Body); err == nil {
		_ = json.Unmarshal(responseBody, &v)
		if token, err = jsonpath.Get("$.access_token", v); err == nil {
			str = token.(string)
		}
	}

	return
}

func Initialize() error {
	if officeToken != "" {
		return nil
	}
	token, err := login(tenantID, clientID, clientSecret, officeResource)
	if err != nil {
		return nil
	}
	officeToken = token
	token, err = login(tenantID, clientID, clientSecret, graphResource)
	if err != nil {
		return nil
	}
	graphToken = token
	log.Info(fmt.Sprintf("initialized MS Graph connection with  %s and %s", officeResource, graphResource))
	return nil
}

func ExecuteRequest(graphUri string, token string) ([]byte, error) {
	request, _ := http.NewRequest("GET", graphUri, nil)
	request.Header.Set("accept", "application/json; odata.metadata=full")
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		log.Info("[MSGraph Connector]:  Retrying Authentication...")
		_ = response.Body.Close()
		isOfficeToken := false
		if token == officeToken {
			isOfficeToken = true
		}
		officeToken = ""
		graphToken = ""
		_ = Initialize()
		newToken := graphToken
		if isOfficeToken {
			newToken = officeToken
		}
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", newToken))
		response, err = httpClient.Do(request)
		if err != nil {
			return nil, err
		}
	}
	defer func() {
		_ = response.Body.Close()
	}()
	return ioutil.ReadAll(response.Body)
}

func Do(request *http.Request) (*http.Response, error) {
	// TODO: retry logics
	return httpClient.Do(request)
}

func parseError(v interface{}) error {
	msg, err := jsonpath.Get("$.error.message", v)
	if err != nil {
		log.Error(err)
		return err
	}
	if msg != nil {
		log.Error(msg)
		return errors.New(msg.(string))
	}
	return nil
}

func createMetric(name string, suffix string, value interface{}) *transit.TimeSeries {
	return createMetricWithThresholds(name, suffix, value, -1, -1)
}

func createMetricWithThresholds(name string, suffix string, value interface{}, warning float64, critical float64) *transit.TimeSeries {
	metricBuilder := connectors.MetricBuilder{
		Name:     fmt.Sprintf("%s%s", name, suffix),
		Value:    value,
		UnitType: transit.UnitCounter,
		Warning:  warning,
		Critical: critical,
		Graphed:  true, // TODO: get this value from configs
	}
	metric, err := connectors.BuildMetric(metricBuilder)
	if err != nil {
		log.Error("failed to build metric " + metricBuilder.Name)
		return nil
	}
	return metric
}

func getCount(v interface{}) (c int, err error) {
	var (
		value interface{}
	)
	if v != nil {
		if value, err = jsonpath.Get("$.value[*]", v); err == nil {
			if value != nil {
				c = len(value.([]interface{}))
				if c == 0 && parseError(v) != nil {
					return
				}
			}
		}
	}
	return
}
