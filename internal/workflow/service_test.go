package workflow

import (
	"sync"
	"testing"
	"time"

	"edna-workbench/internal/domain"
	"edna-workbench/internal/store"
)

func TestServiceIdempotencyAndOptimisticConcurrency(t *testing.T) {
	ledger, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	service := NewService(ledger, func() time.Time { return now })
	command := CreateBatchCommand{
		Meta: CommandMeta{ExpectedVersion: 0, IdempotencyKey: "same-create"}, BatchID: "BATCH-001",
		RiverName: "沱江", SamplingDate: "2026-08-19", Collector: "采样员",
		Sites:   []domain.SamplingSite{{SiteID: "SITE-001", Name: "断面"}},
		Samples: []domain.Sample{{SampleID: "SAMPLE-001", Barcode: "EDNA-0001", SiteID: "SITE-001"}},
	}
	first, err := service.CreateBatch(command)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateBatch(command)
	if err != nil || !second.Receipt.Replayed || second.Receipt.EventID != first.Receipt.EventID {
		t.Fatalf("幂等重放异常: result=%+v err=%v", second, err)
	}
	resultTemplate := RegisterResultCommand{
		Meta: CommandMeta{ExpectedVersion: 1}, BatchID: command.BatchID,
		SampleID: "SAMPLE-001", ExtractionLot: "EXT-001", RunID: "RUN-001", ReadCount: 2000,
		Coverage: .9, NegativeControlRate: .01,
		CandidateTaxa: []domain.CandidateTaxon{{ScientificName: "Pseudorasbora parva", Confidence: .9, ReadCount: 1200, Marker: "12S"}},
	}
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for index := 1; index <= 2; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			candidate := resultTemplate
			candidate.ResultID = "RESULT-00" + string(rune('0'+index))
			candidate.Meta.IdempotencyKey = "result-" + candidate.ResultID
			_, err := service.RegisterResult(candidate)
			results <- err
		}(index)
	}
	wait.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if _, ok := IsConflict(err); ok {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("并发提交应恰有一次成功和一次冲突: success=%d conflict=%d", successes, conflicts)
	}
}
