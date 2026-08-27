package web

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"edna-workbench/internal/domain"
	"edna-workbench/internal/store"
	"edna-workbench/internal/workflow"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	ledger, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	service := workflow.NewService(ledger, func() time.Time { return time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC) })
	return NewServer(service, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
}

func TestWorkbenchAndCreateBatchAPI(t *testing.T) {
	handler := testServer(t)
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/batches/new", nil))
	if page.Code != http.StatusOK || !bytes.Contains(page.Body.Bytes(), []byte("河流 eDNA 样本质量审查工作台")) {
		t.Fatalf("工作台页面不可用: status=%d", page.Code)
	}
	body := workflow.CreateBatchCommand{
		Meta: workflow.CommandMeta{ExpectedVersion: 0, IdempotencyKey: "web-create"}, BatchID: "BATCH-001",
		RiverName: "涪江", SamplingDate: "2026-08-19", Collector: "采样员",
		Sites:   []domain.SamplingSite{{SiteID: "SITE-001", Name: "断面"}},
		Samples: []domain.Sample{{SampleID: "SAMPLE-001", Barcode: "EDNA-0001", SiteID: "SITE-001"}},
	}
	raw, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, "/api/batches", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("创建 API 返回 %d: %s", response.Code, response.Body.String())
	}
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/api/batches/BATCH-001", nil))
	if getResponse.Code != http.StatusOK || !bytes.Contains(getResponse.Body.Bytes(), []byte("BATCH-001")) {
		t.Fatalf("读取批次失败: %d %s", getResponse.Code, getResponse.Body.String())
	}
}

func TestAPIRejectsUnknownFieldAndWrongContentType(t *testing.T) {
	handler := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/batches", bytes.NewBufferString(`{"unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应返回 400，实际 %d", response.Code)
	}
	wrongType := httptest.NewRequest(http.MethodPost, "/api/credentials/verify", bytes.NewBufferString(`{}`))
	wrongType.Header.Set("Content-Type", "text/plain")
	wrongResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongResponse, wrongType)
	if wrongResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("错误 Content-Type 应返回 415，实际 %d", wrongResponse.Code)
	}
}
