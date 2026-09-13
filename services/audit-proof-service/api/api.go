// Package api implements /contracts/openapi/audit-api.v1.yaml.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"soteria/services/audit-proof-service/audit"
)

type Server struct {
	svc *audit.Service
}

func New(svc *audit.Service) *Server { return &Server{svc: svc} }

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/dossiers", s.list)
	mux.HandleFunc("GET /v1/dossiers/{incidentId}", s.get)
	mux.HandleFunc("POST /v1/dossiers/{incidentId}", s.generate)
	mux.HandleFunc("GET /v1/dossiers/{incidentId}/verify", s.verify)
	mux.HandleFunc("GET /v1/dossiers/{incidentId}/chain", s.chain)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "audit-proof-service", "bus": "up"})
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	type row struct {
		IncidentID  string `json:"incident_id"`
		HasDossier  bool   `json:"has_dossier"`
		EventCount  int    `json:"event_count"`
		ContentHash string `json:"content_hash,omitempty"`
	}
	out := []row{}
	for _, id := range s.svc.Incidents() {
		chain, _ := s.svc.Chain(id)
		item := row{IncidentID: id, EventCount: len(chain.Records), ContentHash: chain.Head}
		if _, ok := s.svc.Dossier(id); ok {
			item.HasDossier = true
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// get returns the dossier, as JSON or as the PDF when the id ends in .pdf.
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("incidentId")

	if strings.HasSuffix(id, ".pdf") {
		incident := strings.TrimSuffix(id, ".pdf")
		pdf, ok := s.svc.PDF(incident)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "no dossier has been generated for this incident")
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`inline; filename="soteria-dossier-%s.pdf"`, incident))
		_, _ = w.Write(pdf)
		return
	}

	d, ok := s.svc.Dossier(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no dossier has been generated for this incident")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) generate(w http.ResponseWriter, r *http.Request) {
	d, err := s.svc.Generate(r.Context(), r.PathValue("incidentId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// verify recomputes the chain. It is deliberately a separate endpoint from the
// dossier itself: an auditor should be able to ask "is this still intact?"
// without re-reading the document.
func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	chain, ok := s.svc.Chain(r.PathValue("incidentId"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no events recorded for this incident")
		return
	}
	if err := chain.Verify(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"incident_id": chain.IncidentID, "verified": false,
			"events": len(chain.Records), "problem": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"incident_id": chain.IncidentID, "verified": true,
		"events": len(chain.Records), "content_hash": chain.Head,
		"hash_algorithm": "sha256",
	})
}

// chain hands over the raw records so a recipient can verify with their own code
// rather than taking this service's word for it.
func (s *Server) chain(w http.ResponseWriter, r *http.Request) {
	chain, ok := s.svc.Chain(r.PathValue("incidentId"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no events recorded for this incident")
		return
	}
	writeJSON(w, http.StatusOK, chain)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}

func writeError(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]string{"code": errCode, "message": msg})
}
