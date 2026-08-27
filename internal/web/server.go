package web

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"

	"edna-workbench/internal/workflow"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	service *workflow.Service
	logger  *slog.Logger
	mux     *http.ServeMux
}

type envelope struct {
	Data  any       `json:"data,omitempty"`
	Error *apiError `json:"error,omitempty"`
}

type apiError struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	ExpectedVersion int64  `json:"expectedVersion,omitempty"`
	CurrentVersion  int64  `json:"currentVersion,omitempty"`
}

func NewServer(service *workflow.Service, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{service: service, logger: logger, mux: http.NewServeMux()}
	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.recoverer(s.accessLog(s.securityHeaders(s.mux)))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.HandleWorkbench)
	s.mux.HandleFunc("GET /batches/new", s.HandleWorkbench)
	s.mux.HandleFunc("GET /batches/{batchID}", s.HandleWorkbench)
	s.mux.HandleFunc("GET /batches/{batchID}/quality", s.HandleWorkbench)
	s.mux.HandleFunc("GET /batches/{batchID}/review", s.HandleWorkbench)
	s.mux.HandleFunc("GET /batches/{batchID}/release", s.HandleWorkbench)
	s.mux.HandleFunc("GET /verify", s.HandleWorkbench)
	assets, _ := fs.Sub(staticFiles, "static")
	s.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	s.mux.HandleFunc("GET /api/health", s.HandleHealth)
	s.mux.HandleFunc("GET /api/batches", s.HandleListBatches)
	s.mux.HandleFunc("POST /api/batches", s.HandleCreateBatch)
	s.mux.HandleFunc("GET /api/batches/{batchID}", s.HandleGetBatch)
	s.mux.HandleFunc("GET /api/batches/{batchID}/audit", s.HandleBatchAudit)
	s.mux.HandleFunc("GET /api/batches/{batchID}/release-readiness", s.HandleReleaseReadiness)
	s.mux.HandleFunc("POST /api/batches/{batchID}/results", s.HandleRegisterResult)
	s.mux.HandleFunc("POST /api/batches/{batchID}/quality-check", s.HandleQualityCheck)
	s.mux.HandleFunc("POST /api/batches/{batchID}/retests", s.HandleRetestRequest)
	s.mux.HandleFunc("POST /api/batches/{batchID}/expert-review", s.HandleExpertReview)
	s.mux.HandleFunc("POST /api/batches/{batchID}/freeze", s.HandleFreeze)
	s.mux.HandleFunc("POST /api/batches/{batchID}/credential", s.HandleIssueCredential)
	s.mux.HandleFunc("POST /api/credentials/verify", s.HandleVerifyCredential)
}
