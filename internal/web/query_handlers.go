package web

import (
	"net/http"

	"edna-workbench/internal/workflow"
)

func (s *Server) HandleWorkbench(w http.ResponseWriter, r *http.Request) {
	raw, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "asset_unavailable", "工作台页面暂不可用")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, envelope{Data: s.service.Health()})
}

func (s *Server) HandleListBatches(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, envelope{Data: s.service.ListBatches()})
}

func (s *Server) HandleGetBatch(w http.ResponseWriter, r *http.Request) {
	batch, err := s.service.GetBatch(r.PathValue("batchID"))
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, envelope{Data: map[string]any{"batch": batch, "nextAction": workflow.ExplainState(batch)}})
}

func (s *Server) HandleBatchAudit(w http.ResponseWriter, r *http.Request) {
	events, err := s.service.AuditEvents(r.PathValue("batchID"))
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, envelope{Data: events})
}

func (s *Server) HandleReleaseReadiness(w http.ResponseWriter, r *http.Request) {
	readiness, err := s.service.CheckReleaseReadiness(r.PathValue("batchID"))
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, envelope{Data: readiness})
}
