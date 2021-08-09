package parser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gwos/tcg/connectors"
	"github.com/gwos/tcg/milliseconds"
	"github.com/gwos/tcg/services"
	"github.com/gwos/tcg/transit"
)

var (
	bronxRegexp = regexp.MustCompile(`^(.*?);(.*?);(.*?);(.*?);(.*?);(.*?)\|(.*?)$`)
	nscaRegexp  = regexp.MustCompile(`^(?:(.*?);)?(.*?);(.*?);(.*?);(.*?)\|(.*?)$`)
	// skipping unittype postfix
	perfDataRegexp           = regexp.MustCompile(`^(.*?)=(.*?)(?:\D*?);(.*?)(?:\D*?);(.*?)(?:\D*?);$`)
	perfDataWithMinRegexp    = regexp.MustCompile(`^(.*?)=(.*?)(?:\D*?);(.*?)(?:\D*?);(.*?)(?:\D*?);(.*?)(?:\D*?);$`)
	perfDataWithMinMaxRegexp = regexp.MustCompile(`^(.*?)=(.*?)(?:\D*?);(.*?)(?:\D*?);(.*?)(?:\D*?);(.*?)(?:\D*?);(.*?)(?:\D*?);$`)
)

// DataFormat describes incoming payload
type DataFormat string

// Data formats of the received body
const (
	Bronx   DataFormat = "bronx"
	NSCA    DataFormat = "nsca"
	NSCAAlt DataFormat = "nsca-alt"
)

func ProcessMetrics(ctx context.Context, payload []byte, dataFormat DataFormat) (*[]transit.DynamicMonitoredResource, error) {
	var (
		ctxN               context.Context
		err                error
		monitoredResources *[]transit.DynamicMonitoredResource
		span               services.TraceSpan
	)

	ctxN, span = services.StartTraceSpan(ctx, "connectors", "parseBody")
	monitoredResources, err = parse(payload, dataFormat)

	services.EndTraceSpan(span,
		services.TraceAttrError(err),
		services.TraceAttrPayloadLen(payload),
	)

	if err != nil {
		return nil, err
	}

	if err := connectors.SendMetrics(ctxN, *monitoredResources, nil); err != nil {
		return nil, err
	}

	return monitoredResources, nil
}

func parse(payload []byte, dataFormat DataFormat) (*[]transit.DynamicMonitoredResource, error) {
	metricsLines := strings.Split(string(bytes.Trim(payload, " \n\r")), "\n")

	var (
		monitoredResources        []transit.DynamicMonitoredResource
		serviceNameToMetricsMap   map[string][]transit.TimeSeries
		resourceNameToServicesMap map[string][]transit.DynamicMonitoredService
		err                       error
	)

	switch dataFormat {
	case Bronx:
		serviceNameToMetricsMap, err = getBronxMetrics(metricsLines)
	case NSCA, NSCAAlt:
		serviceNameToMetricsMap, err = getNscaMetrics(metricsLines)
	default:
		return nil, errors.New("unknown data format provided")
	}
	if err != nil {
		return nil, err
	}

	switch dataFormat {
	case Bronx:
		resourceNameToServicesMap, err = getBronxServices(serviceNameToMetricsMap, metricsLines)
	case NSCA, NSCAAlt:
		resourceNameToServicesMap, err = getNscaServices(serviceNameToMetricsMap, metricsLines)
	default:
		return nil, errors.New("unknown data format provided")
	}
	if err != nil {
		return nil, err
	}

	for key, value := range resourceNameToServicesMap {
		monitoredResources = append(monitoredResources, transit.DynamicMonitoredResource{
			BaseResource: transit.BaseResource{
				BaseTransitData: transit.BaseTransitData{
					Name: key,
					Type: transit.Host,
				},
			},
			Status:        connectors.CalculateResourceStatus(value),
			LastCheckTime: milliseconds.MillisecondTimestamp{Time: time.Now()},
			NextCheckTime: milliseconds.MillisecondTimestamp{Time: time.Now().Add(connectors.CheckInterval)},
			Services:      value,
		})
	}

	return &monitoredResources, nil
}

func getTime(str string) (*milliseconds.MillisecondTimestamp, error) {
	i, err := strconv.ParseInt(str, 10, 64)
	if err != nil {
		return nil, err
	}

	i *= int64(time.Millisecond)
	return &milliseconds.MillisecondTimestamp{Time: time.Unix(0, i)}, nil
}

func getStatus(str string) (transit.MonitorStatus, error) {
	switch str {
	case "0":
		return transit.ServiceOk, nil
	case "1":
		return transit.ServiceWarning, nil
	case "2":
		return transit.ServiceUnscheduledCritical, nil
	case "3":
		return transit.ServiceUnknown, nil

	default:
		return "nil", errors.New("unknown status provided")
	}
}

func getNscaMetrics(metricsLines []string) (map[string][]transit.TimeSeries, error) {
	metricsMap := make(map[string][]transit.TimeSeries)
	for _, metric := range metricsLines {
		arr := nscaRegexp.FindStringSubmatch(metric)[1:]

		var timestamp = &milliseconds.MillisecondTimestamp{Time: time.Now()}
		var err error
		if len(arr) > 5 && arr[0] == "" {
			arr = arr[1:]
		} else {
			timestamp, err = getTime(arr[0])
			if err != nil {
				return nil, err
			}
		}

		perfData := arr[len(arr)-1]
		pdArr := strings.Split(strings.TrimSpace(perfData), " ")

		for _, metric := range pdArr {
			var values []string
			switch len(strings.Split(metric, ";")) {
			case 4:
				values = perfDataRegexp.FindStringSubmatch(metric)[1:]
			case 5:
				values = perfDataWithMinRegexp.FindStringSubmatch(metric)[1:]
			case 6:
				values = perfDataWithMinMaxRegexp.FindStringSubmatch(metric)[1:]
			}
			if len(values) < 4 {
				return nil, errors.New("invalid metric format")
			}
			var value, warning, critical float64
			if len(values[1]) > 0 {
				if v, err := strconv.ParseFloat(values[1], 64); err == nil {
					value = v
				} else {
					return nil, err
				}
			}
			if len(values[2]) > 0 {
				if w, err := strconv.ParseFloat(values[2], 64); err == nil {
					warning = w
				} else {
					return nil, err
				}
			}
			if len(values[3]) > 0 {
				if c, err := strconv.ParseFloat(values[3], 64); err == nil {
					critical = c
				} else {
					return nil, err
				}
			}

			timeSeries, err := connectors.BuildMetric(connectors.MetricBuilder{
				Name:           values[0],
				ComputeType:    transit.Query,
				Value:          value,
				UnitType:       transit.MB,
				Warning:        warning,
				Critical:       critical,
				StartTimestamp: timestamp,
				EndTimestamp:   timestamp,
			})
			if err != nil {
				return nil, err
			}
			metricsMap[fmt.Sprintf("%s:%s", arr[len(arr)-5], arr[len(arr)-4])] =
				append(metricsMap[fmt.Sprintf("%s:%s", arr[len(arr)-5], arr[len(arr)-4])], *timeSeries)
		}
	}

	return metricsMap, nil
}

func getNscaServices(metricsMap map[string][]transit.TimeSeries, metricsLines []string) (map[string][]transit.DynamicMonitoredService, error) {
	servicesMap := make(map[string][]transit.DynamicMonitoredService)

	for _, metric := range metricsLines {
		arr := nscaRegexp.FindStringSubmatch(metric)[1:]
		var timestamp = &milliseconds.MillisecondTimestamp{Time: time.Now()}
		var err error
		if len(arr) > 5 && arr[0] == "" {
			arr = arr[1:]
		} else {
			timestamp, err = getTime(arr[0])
			if err != nil {
				return nil, err
			}
		}

		status, err := getStatus(arr[len(arr)-3])
		if err != nil {
			return nil, err
		}

		servicesMap[arr[len(arr)-5]] = append(servicesMap[arr[len(arr)-5]], transit.DynamicMonitoredService{
			BaseTransitData: transit.BaseTransitData{
				Name:  arr[len(arr)-4],
				Type:  transit.Service,
				Owner: arr[len(arr)-5],
			},
			Status:           status,
			LastCheckTime:    *timestamp,
			NextCheckTime:    milliseconds.MillisecondTimestamp{Time: timestamp.Add(connectors.CheckInterval)},
			LastPlugInOutput: arr[len(arr)-2],
			Metrics:          metricsMap[fmt.Sprintf("%s:%s", arr[len(arr)-5], arr[len(arr)-4])],
		})
	}

	return removeDuplicateServices(servicesMap), nil
}

func getBronxMetrics(metricsLines []string) (map[string][]transit.TimeSeries, error) {
	metricsMap := make(map[string][]transit.TimeSeries)
	for _, metric := range metricsLines {
		arr := bronxRegexp.FindStringSubmatch(metric)[1:]

		if len(arr) != 7 {
			return nil, errors.New("invalid metric format")
		}

		timestamp, err := getTime(arr[1])
		if err != nil {
			return nil, err
		}

		perfData := arr[6]
		pdArr := strings.Split(strings.TrimSpace(perfData), " ")
		for _, metric := range pdArr {
			var values []string
			switch len(strings.Split(metric, ";")) {
			case 4:
				values = perfDataRegexp.FindStringSubmatch(metric)[1:]
			case 5:
				values = perfDataWithMinRegexp.FindStringSubmatch(metric)[1:]
			case 6:
				values = perfDataWithMinMaxRegexp.FindStringSubmatch(metric)[1:]
			}
			if len(values) < 4 {
				return nil, errors.New("invalid metric format")
			}
			var value, warning, critical float64
			if len(values[1]) > 0 {
				if v, err := strconv.ParseFloat(values[1], 64); err == nil {
					value = v
				} else {
					return nil, err
				}
			}
			if len(values[2]) > 0 {
				if w, err := strconv.ParseFloat(values[2], 64); err == nil {
					warning = w
				} else {
					return nil, err
				}
			}
			if len(values[3]) > 0 {
				if c, err := strconv.ParseFloat(values[3], 64); err == nil {
					critical = c
				} else {
					return nil, err
				}
			}

			timeSeries, err := connectors.BuildMetric(connectors.MetricBuilder{
				Name:           values[0],
				ComputeType:    transit.Query,
				Value:          value,
				UnitType:       transit.MB,
				Warning:        warning,
				Critical:       critical,
				StartTimestamp: timestamp,
				EndTimestamp:   timestamp,
			})
			if err != nil {
				return nil, err
			}
			metricsMap[fmt.Sprintf("%s:%s", arr[2], arr[3])] =
				append(metricsMap[fmt.Sprintf("%s:%s", arr[2], arr[3])], *timeSeries)
		}
	}
	return metricsMap, nil
}

func getBronxServices(metricsMap map[string][]transit.TimeSeries, metricsLines []string) (map[string][]transit.DynamicMonitoredService, error) {
	servicesMap := make(map[string][]transit.DynamicMonitoredService)
	for _, metric := range metricsLines {
		arr := bronxRegexp.FindStringSubmatch(metric)[1:]

		if len(arr) != 7 {
			return nil, errors.New("invalid metric format")
		}

		timestamp, err := getTime(arr[1])
		if err != nil {
			return nil, err
		}

		status, err := getStatus(arr[4])
		if err != nil {
			return nil, err
		}

		servicesMap[arr[2]] = append(servicesMap[arr[2]], transit.DynamicMonitoredService{
			BaseTransitData: transit.BaseTransitData{
				Name:  arr[3],
				Type:  transit.Service,
				Owner: arr[2],
			},
			Status:           status,
			LastCheckTime:    *timestamp,
			NextCheckTime:    milliseconds.MillisecondTimestamp{Time: timestamp.Add(connectors.CheckInterval)},
			LastPlugInOutput: arr[5],
			Metrics:          metricsMap[fmt.Sprintf("%s:%s", arr[2], arr[3])],
		})
	}

	return removeDuplicateServices(servicesMap), nil
}

func removeDuplicateServices(servicesMap map[string][]transit.DynamicMonitoredService) map[string][]transit.DynamicMonitoredService {
	for key, value := range servicesMap {
		keys := make(map[string]bool)
		var list []transit.DynamicMonitoredService
		for _, entry := range value {
			if _, value := keys[entry.Name]; !value {
				keys[entry.Name] = true
				list = append(list, entry)
			}
		}
		servicesMap[key] = list
	}
	return servicesMap
}
