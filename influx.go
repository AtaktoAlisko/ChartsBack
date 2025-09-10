package main

import (
	"context"
	"fmt"
	"os"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

func queryData(imei string, start, stop time.Time) ([]map[string]interface{}, error) {
	// Подключение к InfluxDB
	client := influxdb2.NewClient(os.Getenv("INFLUX_URL"), os.Getenv("INFLUX_TOKEN"))
	defer client.Close()

	queryAPI := client.QueryAPI(os.Getenv("INFLUX_ORG"))

	query := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: %s, stop: %s)
			|> filter(fn: (r) => r._measurement == "%s")
			|> filter(fn: (r) => r.imei == "%s")
	`,
		os.Getenv("INFLUX_BUCKET"),
		start.Format(time.RFC3339),
		stop.Format(time.RFC3339),
		os.Getenv("INFLUX_MEASUREMENT"),
		imei,
	)

	result, err := queryAPI.Query(context.Background(), query)
	if err != nil {
		return nil, err
	}

	var data []map[string]interface{}
	for result.Next() {
		data = append(data, map[string]interface{}{
			"time":  result.Record().Time(),
			"field": result.Record().Field(),
			"value": result.Record().Value(),
		})
	}
	if result.Err() != nil {
		return nil, result.Err()
	}
	return data, nil
}
