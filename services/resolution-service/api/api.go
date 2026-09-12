// Package api implements /contracts/openapi/resolution-api.v1.yaml.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"soteria/libs/core/events"
	"soteria/services/resolution-service/resolver"
)

type Server struct {
	store *resolver.Store
	// busUp reports broker connectivity for /healthz.
	busUp func() bool
}

func New(store *resolver.Store, busUp func() bool) *Server {
	if busUp == nil {
		busUp = func() bool { return true }
	}
	return &Server{store: store, busUp: busUp}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/lots/status", s.lotStatus)
	mux.HandleFunc("GET /v1/resolutions", s.listResolutions)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	status, busState := "ok", "up"
	if !s.busUp() {
		status, busState = "degraded", "down"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status, "service": "resolution-service", "bus": busState})
}

func (s *Server) lotStatus(w http.ResponseWriter, r *http.Request) {
	gtin := r.URL.Query().Get("gtin")
	if gtin == "" {
		writeError(w, http.StatusBadRequest, "missing_gtin", "gtin query parameter is required")
		return
	}
	writeJSON(w, http.StatusOK, s.store.LotStatus(gtin, r.URL.Query().Get("lot_code")))
}

func (s *Server) listResolutions(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	items := s.store.List(r.URL.Query().Get("incident_id"), r.URL.Query().Get("gtin"), limit)
	if items == nil {
		items = []events.LotResolved{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]string{"code": errCode, "message": msg})
}
