package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/joho/godotenv"
)

type ImeiResponse struct {
	IMEIs []string `json:"imeis"`
}

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️ .env не найден, читаем переменные окружения напрямую")
	}

	url := os.Getenv("INFLUX_URL")
	token := os.Getenv("INFLUX_TOKEN")
	org := os.Getenv("INFLUX_ORG")
	bucket := os.Getenv("INFLUX_BUCKET")

	if url == "" || token == "" || org == "" || bucket == "" {
		log.Fatal("❌ Проверь .env — не хватает переменных INFLUX_URL / INFLUX_TOKEN / INFLUX_ORG / INFLUX_BUCKET")
	}

	client := influxdb2.NewClient(url, token)
	defer client.Close()

	health, err := client.Health(context.Background())
	if err != nil {
		log.Fatalf("❌ Ошибка при подключении к InfluxDB: %v", err)
	}
	fmt.Printf("✅ InfluxDB статус: %s\n", *health.Message)

	queryAPI := client.QueryAPI(org)

	http.HandleFunc("/api/imeis", func(w http.ResponseWriter, r *http.Request) {
		query := fmt.Sprintf(`
			from(bucket: "%s")
				|> range(start: -30d)
				|> filter(fn: (r) => r["_measurement"] == "telemetry")
				|> keep(columns: ["imei"])
				|> group()
				|> distinct(column: "imei")
				|> sort(columns: ["imei"])
		`, bucket)

		result, err := queryAPI.Query(context.Background(), query)
		if err != nil {
			http.Error(w, "Query error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer result.Close()

		var imeis []string
		for result.Next() {
			if v, ok := result.Record().Value().(string); ok {
				imeis = append(imeis, v)
			}
		}

		if result.Err() != nil {
			http.Error(w, "Flux parsing error: "+result.Err().Error(), http.StatusInternalServerError)
			return
		}

		resp := ImeiResponse{IMEIs: imeis}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	http.HandleFunc("/api/fields", func(w http.ResponseWriter, r *http.Request) {
		imei := r.URL.Query().Get("imei")
		if imei == "" {
			http.Error(w, "imei обязателен", http.StatusBadRequest)
			return
		}

		query := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: -30d)
			|> filter(fn: (r) => r["_measurement"] == "telemetry")
			|> filter(fn: (r) => r["imei"] == "%s")
			|> keep(columns: ["_field"])
			|> group()
			|> distinct(column: "_field")
			|> sort(columns: ["_field"])
	`, bucket, imei)

		result, err := queryAPI.Query(context.Background(), query)
		if err != nil {
			http.Error(w, "Query error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer result.Close()

		var fields []string
		for result.Next() {
			if v, ok := result.Record().Value().(string); ok {
				fields = append(fields, v)
			}
		}

		if result.Err() != nil {
			http.Error(w, "Flux parsing error: "+result.Err().Error(), http.StatusInternalServerError)
			return
		}

		resp := map[string][]string{"fields": fields}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	http.HandleFunc("/api/telemetry", func(w http.ResponseWriter, r *http.Request) {
		imei := r.URL.Query().Get("imei")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if imei == "" || start == "" || end == "" {
			http.Error(w, "imei, start, end обязательны", http.StatusBadRequest)
			return
		}

		query := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: %s, stop: %s)
			|> filter(fn: (r) => r["_measurement"] == "telemetry" and r["imei"] == "%s")
			|> filter(fn: (r) => r["_field"] == "speed" or r["_field"] == "fls485_level_2" or r["_field"] == "latitude" or r["_field"] == "longitude" or r["_field"] == "main_power_voltage" or r["_field"] == "event_time")
			|> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
			|> sort(columns: ["_time"])
			|> map(fn: (r) => ({
				r with
				main_power_voltage: if exists r.main_power_voltage then float(v: r.main_power_voltage) / 1000.0 else float(v: 0.0)
			}))
	`, bucket, start, end, imei)

		result, err := queryAPI.Query(context.Background(), query)
		if err != nil {
			http.Error(w, "Query error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer result.Close()

		series := map[string][]map[string]interface{}{
			"speed":              {},
			"fls485_level_2":     {},
			"main_power_voltage": {},
		}
		track := []map[string]interface{}{}

		for result.Next() {
			rec := result.Record()
			t := rec.Time().UTC().Format(time.RFC3339)

			// speed
			if v, ok := rec.ValueByKey("speed").(int64); ok {
				series["speed"] = append(series["speed"], map[string]interface{}{"time": t, "value": v})
			}

			// fls485_level_2
			if v, ok := rec.ValueByKey("fls485_level_2").(int64); ok {
				series["fls485_level_2"] = append(series["fls485_level_2"], map[string]interface{}{"time": t, "value": v})
			}

			// main_power_voltage (float)
			if v, ok := rec.ValueByKey("main_power_voltage").(float64); ok {
				series["main_power_voltage"] = append(series["main_power_voltage"], map[string]interface{}{"time": t, "value": v})
			}

			// track (lat/lon)
			lat, latOK := rec.ValueByKey("latitude").(float64)
			lon, lonOK := rec.ValueByKey("longitude").(float64)
			eventTime, _ := rec.ValueByKey("event_time").(int64)
			if latOK && lonOK {
				track = append(track, map[string]interface{}{
					"time":       t,
					"lat":        lat,
					"lon":        lon,
					"event_time": eventTime,
				})
			}
		}

		resp := map[string]interface{}{
			"series": series,
			"track":  track,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	fmt.Println("🚀 Сервер запущен: http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
