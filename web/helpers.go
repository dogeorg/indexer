package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// sendOptions sends a response to an OPTIONS request.
func sendOptions(w http.ResponseWriter, r *http.Request, options string, corsOrigin string) {
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Allow", options)
		w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
		w.Header().Set("Access-Control-Allow-Methods", options)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Allow", options)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// sendJson sends a JSON response to a web request.
func sendJson(w http.ResponseWriter, payload any, options string, corsOrigin string) {
	bytes, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("error encoding JSON: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "private; max-age=0")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	w.Header().Set("Allow", options)
	w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
	w.Header().Set("Access-Control-Allow-Methods", options)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Write(bytes)
}

type WebError struct {
	Error  string `json:"error"`
	Reason string `json:"reason"`
}

// sendError sends a json error response to a web request.
func sendError(w http.ResponseWriter, statusCode int, code string, reason string, options string, corsOrigin string) {
	bytes, err := json.Marshal(WebError{Error: code, Reason: reason})
	if err != nil {
		bytes = []byte(fmt.Sprintf("{\"error\":\"json\",\"reason\":\"encoding JSON: %s\"}", err.Error()))
		statusCode = http.StatusInternalServerError
	}
	w.Header().Set("Cache-Control", "private; max-age=0")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	w.Header().Set("Allow", options)
	w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
	w.Header().Set("Access-Control-Allow-Methods", options)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(statusCode)
	w.Write(bytes)
}
