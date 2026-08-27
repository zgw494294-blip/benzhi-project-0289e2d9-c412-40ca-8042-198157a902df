package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type SamplingSite struct {
	SiteID      string  `json:"siteID"`
	Name        string  `json:"name"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Description string  `json:"description,omitempty"`
}

type Sample struct {
	SampleID  string `json:"sampleID"`
	Barcode   string `json:"barcode"`
	SiteID    string `json:"siteID"`
	Matrix    string `json:"matrix"`
	Collected string `json:"collectedAt"`
}

type NewBatchInput struct {
	BatchID      string
	RiverName    string
	SamplingDate string
	Collector    string
	Sites        []SamplingSite
	Samples      []Sample
}

var barcodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{3,31}$`)
var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{2,63}$`)

func NewSamplingBatch(input NewBatchInput, now time.Time) (*SamplingBatch, error) {
	input.BatchID = strings.TrimSpace(input.BatchID)
	input.RiverName = strings.TrimSpace(input.RiverName)
	input.Collector = strings.TrimSpace(input.Collector)
	if !identifierPattern.MatchString(input.BatchID) {
		return nil, errors.New("批次编号格式无效，仅允许字母、数字、下划线和连字符")
	}
	if input.RiverName == "" || input.Collector == "" {
		return nil, errors.New("河流名称和采样员不能为空")
	}
	date, err := time.Parse("2006-01-02", input.SamplingDate)
	if err != nil || date.After(now.Add(24*time.Hour)) {
		return nil, errors.New("采样日期无效或晚于当前日期")
	}
	if len(input.Sites) == 0 || len(input.Samples) == 0 {
		return nil, errors.New("批次至少需要一个采样点和一个样本")
	}
	if err := validateSitesAndSamples(input.Sites, input.Samples); err != nil {
		return nil, err
	}
	return &SamplingBatch{
		BatchID: input.BatchID, RiverName: input.RiverName, SamplingDate: input.SamplingDate,
		Collector: input.Collector, Sites: cloneSlice(input.Sites), Samples: cloneSlice(input.Samples),
		Status: BatchDraft, Version: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}, nil
}

func validateSitesAndSamples(sites []SamplingSite, samples []Sample) error {
	siteIDs := make(map[string]struct{}, len(sites))
	for i := range sites {
		sites[i].SiteID = strings.TrimSpace(sites[i].SiteID)
		sites[i].Name = strings.TrimSpace(sites[i].Name)
		if !identifierPattern.MatchString(sites[i].SiteID) || sites[i].Name == "" {
			return fmt.Errorf("采样点 %d 的编号或名称无效", i+1)
		}
		if sites[i].Latitude < -90 || sites[i].Latitude > 90 || sites[i].Longitude < -180 || sites[i].Longitude > 180 {
			return fmt.Errorf("采样点 %s 的经纬度超出范围", sites[i].SiteID)
		}
		if _, exists := siteIDs[sites[i].SiteID]; exists {
			return fmt.Errorf("采样点编号重复: %s", sites[i].SiteID)
		}
		siteIDs[sites[i].SiteID] = struct{}{}
	}
	barcodes := make(map[string]struct{}, len(samples))
	sampleIDs := make(map[string]struct{}, len(samples))
	for i := range samples {
		if !identifierPattern.MatchString(samples[i].SampleID) || !barcodePattern.MatchString(samples[i].Barcode) {
			return fmt.Errorf("样本 %d 的编号或条码格式无效", i+1)
		}
		if _, ok := siteIDs[samples[i].SiteID]; !ok {
			return fmt.Errorf("样本 %s 关联了不存在的采样点", samples[i].SampleID)
		}
		if _, exists := sampleIDs[samples[i].SampleID]; exists {
			return fmt.Errorf("样本编号重复: %s", samples[i].SampleID)
		}
		if _, exists := barcodes[samples[i].Barcode]; exists {
			return fmt.Errorf("样本条码重复: %s", samples[i].Barcode)
		}
		sampleIDs[samples[i].SampleID] = struct{}{}
		barcodes[samples[i].Barcode] = struct{}{}
	}
	return nil
}
