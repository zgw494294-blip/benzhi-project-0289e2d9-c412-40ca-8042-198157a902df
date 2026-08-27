package domain

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (b *SamplingBatch) Freeze(frozenBy string, now time.Time) (*FrozenSnapshot, error) {
	if b.Status != BatchApproved || b.Review == nil || b.Review.Decision != DecisionApproved {
		return nil, errors.New("只有专家复核通过的批次可以冻结")
	}
	if strings.TrimSpace(frozenBy) == "" {
		return nil, errors.New("冻结负责人不能为空")
	}
	taxaByName := make(map[string]*FrozenTaxon)
	for _, result := range b.Results {
		if result.Status != ResultActive {
			continue
		}
		for _, candidate := range result.CandidateTaxa {
			if candidate.Confidence < 0.80 {
				continue
			}
			key := strings.ToLower(candidate.ScientificName)
			entry := taxaByName[key]
			if entry == nil {
				entry = &FrozenTaxon{ScientificName: candidate.ScientificName, CommonName: candidate.CommonName}
				taxaByName[key] = entry
			}
			entry.Markers = appendUnique(entry.Markers, candidate.Marker)
			entry.SampleIDs = appendUnique(entry.SampleIDs, result.SampleID)
			entry.EvidenceDigests = appendUnique(entry.EvidenceDigests, result.EvidenceDigest)
			if candidate.Confidence > entry.MaxConfidence {
				entry.MaxConfidence = candidate.Confidence
			}
		}
	}
	if len(taxaByName) == 0 {
		return nil, errors.New("没有置信度达到 0.80 的有效物种候选，无法冻结")
	}
	taxa := make([]FrozenTaxon, 0, len(taxaByName))
	for _, entry := range taxaByName {
		sort.Strings(entry.Markers)
		sort.Strings(entry.SampleIDs)
		sort.Strings(entry.EvidenceDigests)
		taxa = append(taxa, *entry)
	}
	sort.Slice(taxa, func(i, j int) bool { return taxa[i].ScientificName < taxa[j].ScientificName })
	snapshot := FrozenSnapshot{
		SchemaVersion: 1, BatchID: b.BatchID, BatchVersion: b.Version + 1,
		RiverName: b.RiverName, SamplingDate: b.SamplingDate, Taxa: taxa,
		EvidenceSummary: fmt.Sprintf("%d 个物种，来自 %d 个样本和 %d 条测序结果", len(taxa), len(b.Samples), countActiveResults(b.Results)),
		FrozenBy:        strings.TrimSpace(frozenBy), FrozenAt: now.UTC(),
	}
	digest, err := SnapshotDigest(snapshot)
	if err != nil {
		return nil, err
	}
	snapshot.Digest = digest
	b.Snapshot = &snapshot
	b.Status = BatchFrozen
	b.Touch(now)
	return b.Snapshot, nil
}

func SnapshotDigest(snapshot FrozenSnapshot) (string, error) {
	snapshot.Digest = ""
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("序列化冻结快照: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (b *SamplingBatch) IssueCredential(credentialID, issuedBy string, now time.Time) (*ReleaseCredential, error) {
	if b.Status != BatchFrozen || b.Snapshot == nil {
		return nil, errors.New("只有已冻结且尚未发布的批次可以签发凭据")
	}
	if !identifierPattern.MatchString(credentialID) || strings.TrimSpace(issuedBy) == "" {
		return nil, errors.New("凭据编号格式无效或签发人为空")
	}
	credential := ReleaseCredential{
		CredentialID: credentialID, BatchID: b.BatchID, SnapshotDigest: b.Snapshot.Digest,
		TaxaCount: len(b.Snapshot.Taxa), IssuedBy: strings.TrimSpace(issuedBy), IssuedAt: now.UTC(), State: CredentialValid,
	}
	credential.VerificationCode = VerificationCode(credential)
	b.Credential = &credential
	b.Status = BatchReleased
	b.Touch(now)
	return b.Credential, nil
}

func VerificationCode(credential ReleaseCredential) string {
	canonical := strings.Join([]string{credential.CredentialID, credential.BatchID, credential.SnapshotDigest, fmt.Sprint(credential.TaxaCount), credential.IssuedBy, credential.IssuedAt.UTC().Format(time.RFC3339Nano)}, "|")
	sum := sha256.Sum256([]byte(canonical))
	return strings.ToUpper(hex.EncodeToString(sum[:8]))
}

func VerifyCredential(credential ReleaseCredential, suppliedDigest, suppliedCode string) (bool, string) {
	if credential.State != CredentialValid {
		return false, "凭据已失效或撤销"
	}
	if suppliedDigest != "" && subtle.ConstantTimeCompare([]byte(strings.ToLower(suppliedDigest)), []byte(strings.ToLower(credential.SnapshotDigest))) != 1 {
		return false, "提交的快照摘要与签发记录不一致"
	}
	expected := VerificationCode(credential)
	code := credential.VerificationCode
	if suppliedCode != "" {
		code = strings.ToUpper(strings.TrimSpace(suppliedCode))
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) != 1 {
		return false, "验证码不匹配，凭据内容可能已被修改"
	}
	return true, "凭据有效，摘要和签发信息完整"
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func countActiveResults(results []SequencingResult) int {
	count := 0
	for _, result := range results {
		if result.Status == ResultActive {
			count++
		}
	}
	return count
}
