// Package api implements /contracts/openapi/order-rescue-api.v1.yaml.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"soteria/services/order-rescue-service/rescue"
)

type Server struct {
	svc   *rescue.Service
	busUp func() bool
}

func New(svc *rescue.Service, busUp func() bool) *Server {
	if busUp == nil {
		busUp = func() bool { return true }
	}
	return &Server{svc: svc, busUp: busUp}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/rescues", s.list)
	mux.HandleFunc("GET /v1/rescues/{rescueId}", s.get)
	mux.HandleFunc("POST /v1/rescues/{rescueId}/confirm", s.confirm)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	status, busState := "ok", "up"
	if !s.busUp() {
		status, busState = "degraded", "down"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status, "service": "order-rescue-service", "bus": busState})
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items := s.svc.List(q.Get("order_id"), q.Get("incident_id"), q.Get("status"))
	if items == nil {
		items = []rescue.Record{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.svc.Get(r.PathValue("rescueId"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such rescue")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) confirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OptionID     string `json:"option_id"`
		ConsentToken string `json:"consent_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.OptionID == "" || body.ConsentToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "option_id and consent_token are required")
		return
	}
	rec, err := s.svc.Confirm(r.Context(), r.PathValue("rescueId"), body.OptionID, body.ConsentToken)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, rec)
	case errors.Is(err, rescue.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such rescue")
	case errors.Is(err, rescue.ErrBadConsent):
		writeError(w, http.StatusForbidden, "invalid_consent", "consent token does not match this rescue")
	case errors.Is(err, rescue.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "this rescue is already confirmed or has expired")
	case errors.Is(err, rescue.ErrUnknownOption):
		writeError(w, http.StatusBadRequest, "unknown_option", "that option is not offered on this rescue")
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
