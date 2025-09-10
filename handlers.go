package main

import (
	"encoding/json"
	"net/http"
	"time"
)

func getDataHandler(w http.ResponseWriter, r *http.Request) {
	imei := r.URL.Query().Get("imei")
	startStr := r.URL.Query().Get("start")
	stopStr := r.URL.Query().Get("stop")

	if imei == "" || startStr == "" || stopStr == "" {
		http.Error(w, "imei, start и stop обязательны", http.StatusBadRequest)
		return
	}

	start, err1 := time.Parse(time.RFC3339, startStr)
	stop, err2 := time.Parse(time.RFC3339, stopStr)
	if err1 != nil || err2 != nil {
		http.Error(w, "Неверный формат времени", http.StatusBadRequest)
		return
	}

	data, err := queryData(imei, start, stop)
	if err != nil {
		http.Error(w, "Ошибка при запросе к InfluxDB: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
