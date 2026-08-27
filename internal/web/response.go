package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"edna-workbench/internal/store"
	"edna-workbench/internal/workflow"
)

func (s *Server) execute(w http.ResponseWriter, operation func() (workflow.CommandResult, error)) {
	result, err := operation()
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, envelope{Data: result})
}

func (s *Server) decodeCommand(w http.ResponseWriter, r *http.Request, destination any) bool {
	if r.Header.Get("Content-Type") != "" && !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		s.writeError(w, http.StatusUnsupportedMediaType, "content_type", "请求 Content-Type 必须为 application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		message := "请求 JSON 格式无效"
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			message = "请求体不能超过 1 MiB"
		}
		s.writeError(w, http.StatusBadRequest, "invalid_request", message+": "+err.Error())
		return false
	}
	if err := ensureJSONEnd(decoder); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "请求体只能包含一个 JSON 对象")
		return false
	}
	return true
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("存在额外 JSON 值")
	}
	return err
}

func applyIdempotencyHeader(r *http.Request, meta *workflow.CommandMeta) {
	if header := strings.TrimSpace(r.Header.Get("Idempotency-Key")); header != "" {
		meta.IdempotencyKey = header
	}
}

func (s *Server) handleServiceError(w http.ResponseWriter, err error) {
	if conflict, ok := workflow.IsConflict(err); ok {
		s.writeJSON(w, http.StatusConflict, envelope{Error: &apiError{
			Code: "version_conflict", Message: conflict.Error(),
			ExpectedVersion: conflict.Expected, CurrentVersion: conflict.Actual,
		}})
		return
	}
	if workflow.IsNotFound(err) {
		s.writeError(w, http.StatusNotFound, "not_found", "未找到指定记录")
		return
	}
	s.writeError(w, http.StatusUnprocessableEntity, "business_rule", err.Error())
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, envelope{Error: &apiError{Code: code, Message: message}})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		s.logger.Error("写入 JSON 响应失败", "error", err)
	}
}

func IsLedgerError(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}
