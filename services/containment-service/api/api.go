// Package api implements /contracts/openapi/containment-api.v1.yaml.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"soteria/services/containment-service/containment"
)

type Server struct {
	svc   *containment.Service
	busUp func() bool
}

func New(svc *containment.Service, busUp func() bool) *Server {
	if busUp == nil {
		busUp = func() bool { return true }
	}
	return &Server{svc: svc, busUp: busUp}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/containment/actions", s.list)
	mux.HandleFunc("GET /v1/containment/actions/{actionId}", s.get)
	mux.HandleFunc("POST /v1/containment/actions/{actionId}/confirm", s.confirm)
	mux.HandleFunc("POST /v1/containment/actions/{actionId}/reject", s.reject)
	mux.HandleFunc("GET /v1/containment/config", s.getConfig)
	mux.HandleFunc("PUT /v1/containment/config", s.putConfig)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	status, busState := "ok", "up"
	if !s.busUp() {
		status, busState = "degraded", "down"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status, "service": "containment-service", "bus": busState})
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	items := s.svc.Store().List(r.URL.Query().Get("status"), r.URL.Query().Get("incident_id"), limit)
	if items == nil {
		items = []containment.Action{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	action, ok := s.svc.Store().Get(r.PathValue("actionId"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such containment action")
		return
	}
	writeJSON(w, http.StatusOK, action)
}

func (s *Server) confirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Actor            string   `json:"actor"`
		Note             string   `json:"note"`
		LotCodesOverride []string `json:"lot_codes_override"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Actor == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "actor is required")
		return
	}
	action, err := s.svc.Confirm(r.Context(), r.PathValue("actionId"), body.Actor, body.Note, body.LotCodesOverride)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, action)
}

func (s *Server) reject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Actor  string `json:"actor"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Actor == "" || body.Reason == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "actor and reason are required")
		return
	}
	action, err := s.svc.Reject(r.Context(), r.PathValue("actionId"), body.Actor, body.Reason)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, action)
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.Store().Config())
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AutoHoldThreshold float64 `json:"auto_hold_threshold"`
		SKUScopeThreshold float64 `json:"sku_scope_threshold"`
		Actor             string  `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "malformed JSON")
		return
	}
	if body.Actor == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "actor is required")
		return
	}
	if body.AutoHoldThreshold <= 0 || body.AutoHoldThreshold > 1 {
		writeError(w, http.StatusBadRequest, "invalid_threshold", "auto_hold_threshold must be in (0, 1]")
		return
	}
	if body.SKUScopeThreshold < 0 || body.SKUScopeThreshold > 1 {
		writeError(w, http.StatusBadRequest, "invalid_threshold", "sku_scope_threshold must be in [0, 1]")
		return
	}
	cfg := s.svc.UpdateConfig(containment.Config{
		AutoHoldThreshold: body.AutoHoldThreshold,
		SKUScopeThreshold: body.SKUScopeThreshold,
		UpdatedBy:         body.Actor,
	})
	writeJSON(w, http.StatusOK, cfg)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, containment.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such containment action")
	case errors.Is(err, containment.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "action is not awaiting review")
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]string{"code": errCode, "message": msg})
}
