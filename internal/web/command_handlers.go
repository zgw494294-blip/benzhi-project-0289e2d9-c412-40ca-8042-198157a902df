package web

import (
	"net/http"

	"edna-workbench/internal/workflow"
)

func (s *Server) HandleCreateBatch(w http.ResponseWriter, r *http.Request) {
	var command workflow.CreateBatchCommand
	if !s.decodeCommand(w, r, &command) {
		return
	}
	applyIdempotencyHeader(r, &command.Meta)
	result, err := s.service.CreateBatch(command)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Receipt.Replayed {
		status = http.StatusOK
	}
	s.writeJSON(w, status, envelope{Data: result})
}

func (s *Server) HandleRegisterResult(w http.ResponseWriter, r *http.Request) {
	var command workflow.RegisterResultCommand
	if !s.decodeCommand(w, r, &command) {
		return
	}
	command.BatchID = r.PathValue("batchID")
	applyIdempotencyHeader(r, &command.Meta)
	s.execute(w, func() (workflow.CommandResult, error) {
		return s.service.RegisterResultContext(r.Context(), command)
	})
}

func (s *Server) HandleQualityCheck(w http.ResponseWriter, r *http.Request) {
	var command workflow.QualityCheckCommand
	if !s.decodeCommand(w, r, &command) {
		return
	}
	command.BatchID = r.PathValue("batchID")
	applyIdempotencyHeader(r, &command.Meta)
	s.execute(w, func() (workflow.CommandResult, error) { return s.service.RunQualityCheck(command) })
}

func (s *Server) HandleRetestRequest(w http.ResponseWriter, r *http.Request) {
	var command workflow.RetestCommand
	if !s.decodeCommand(w, r, &command) {
		return
	}
	command.BatchID = r.PathValue("batchID")
	applyIdempotencyHeader(r, &command.Meta)
	s.execute(w, func() (workflow.CommandResult, error) { return s.service.RequestRetest(command) })
}

func (s *Server) HandleExpertReview(w http.ResponseWriter, r *http.Request) {
	var command workflow.ExpertReviewCommand
	if !s.decodeCommand(w, r, &command) {
		return
	}
	command.BatchID = r.PathValue("batchID")
	applyIdempotencyHeader(r, &command.Meta)
	s.execute(w, func() (workflow.CommandResult, error) { return s.service.SubmitExpertReview(command) })
}

func (s *Server) HandleFreeze(w http.ResponseWriter, r *http.Request) {
	var command workflow.FreezeCommand
	if !s.decodeCommand(w, r, &command) {
		return
	}
	command.BatchID = r.PathValue("batchID")
	applyIdempotencyHeader(r, &command.Meta)
	s.execute(w, func() (workflow.CommandResult, error) { return s.service.Freeze(command) })
}

func (s *Server) HandleIssueCredential(w http.ResponseWriter, r *http.Request) {
	var command workflow.IssueCredentialCommand
	if !s.decodeCommand(w, r, &command) {
		return
	}
	command.BatchID = r.PathValue("batchID")
	applyIdempotencyHeader(r, &command.Meta)
	s.execute(w, func() (workflow.CommandResult, error) { return s.service.IssueCredential(command) })
}

func (s *Server) HandleVerifyCredential(w http.ResponseWriter, r *http.Request) {
	var request workflow.VerificationRequest
	if !s.decodeCommand(w, r, &request) {
		return
	}
	result, err := s.service.VerifyCredential(request)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, envelope{Data: result})
}
