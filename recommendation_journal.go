package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxJournalRecordBytes = 2 << 20

type TokenUsage struct {
	InputTokens          int64 `json:"input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	CostUpperNanodollars int64 `json:"cost_upper_nanodollars"` // Uncached rates; reasoning included.
}

type RecommendationRecord struct {
	Version              int                   `json:"version"`
	ID                   string                `json:"id"`
	CreatedAt            time.Time             `json:"created_at"`
	AccountID            string                `json:"account_id"`
	Symbol               string                `json:"symbol"`
	Model                string                `json:"model"`
	PromptVersion        string                `json:"prompt_version"`
	Input                RecommendationContext `json:"input"`
	Status               string                `json:"status"` // reserved, failed, invalid, or validated; never an execution status.
	Recommendation       *Decision             `json:"recommendation"`
	Validation           ValidationReport      `json:"validation"`
	BudgetNanodollars    int64                 `json:"budget_nanodollars"`
	ReservedNanodollars  int64                 `json:"reserved_nanodollars"`
	InputTokenUpperBound int64                 `json:"input_token_upper_bound"`
	Usage                *TokenUsage           `json:"usage"` // Null means unknown/potentially billed, not zero cost.
	Failure              string                `json:"failure,omitempty"`
}

type RecommendationJournal interface {
	Latest(accountID string, symbols []string) (map[string]*PreviousRecommendation, error)
	Save(RecommendationRecord) error
}

type FileRecommendationJournal struct{ Directory string }

func newRecordID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("create recommendation ID: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func (j FileRecommendationJournal) accountDirectory(accountID string) string {
	hash := sha256.Sum256([]byte(accountID))
	return filepath.Join(j.Directory, hex.EncodeToString(hash[:]))
}

func (j FileRecommendationJournal) Latest(accountID string, symbols []string) (map[string]*PreviousRecommendation, error) {
	if strings.TrimSpace(j.Directory) == "" || accountID == "" {
		return nil, fmt.Errorf("journal directory and account ID are required")
	}
	latest := make(map[string]*PreviousRecommendation, len(symbols))
	for _, symbol := range symbols {
		latest[symbol] = nil
	}
	dir := j.accountDirectory(accountID)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return latest, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read recommendation journal: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		} // Atomic-write temporary files are not records.
		if !entry.Type().IsRegular() {
			return nil, fmt.Errorf("journal record must be a regular file")
		}
		record, err := readRecommendationRecord(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if record.AccountID != accountID || record.ID+".json" != entry.Name() {
			return nil, fmt.Errorf("journal record identity mismatch")
		}
		if record.Status != "validated" {
			continue
		}
		if _, wanted := latest[record.Symbol]; !wanted {
			continue
		}
		old := latest[record.Symbol]
		if old == nil || record.CreatedAt.After(old.CreatedAt) || (record.CreatedAt.Equal(old.CreatedAt) && record.ID > old.ID) {
			latest[record.Symbol] = &PreviousRecommendation{ID: record.ID, CreatedAt: record.CreatedAt, Model: record.Model, PromptVersion: record.PromptVersion, Decision: *record.Recommendation}
		}
	}
	return latest, nil
}

func readRecommendationRecord(path string) (RecommendationRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return RecommendationRecord{}, fmt.Errorf("open journal record: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(f, maxJournalRecordBytes+1))
	closeErr := f.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return RecommendationRecord{}, fmt.Errorf("read journal record: %w", err)
	}
	if len(data) > maxJournalRecordBytes {
		return RecommendationRecord{}, fmt.Errorf("journal record exceeds size limit")
	}
	var record RecommendationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return record, fmt.Errorf("corrupt recommendation journal JSON")
	}
	if err := validateJournalRecord(record); err != nil {
		return record, err
	}
	return record, nil
}

func validateJournalRecord(r RecommendationRecord) error {
	id, err := hex.DecodeString(r.ID)
	// Keep pre-planner recommendations readable; their original context and
	// validation messages are historical facts, not the current program policy.
	knownPrompt := r.PromptVersion == "loss-aware-v1" || r.PromptVersion == promptVersion
	if err != nil || len(id) != 16 || r.Version != 1 || r.CreatedAt.IsZero() || r.AccountID == "" || r.Symbol == "" || r.Input.Symbol != r.Symbol || r.Input.Snapshot.Account.ID != r.AccountID || r.Model != approvedModel || !knownPrompt {
		return fmt.Errorf("corrupt or unsupported recommendation journal record")
	}
	if r.InputTokenUpperBound < aiFramingTokenAllowance || r.InputTokenUpperBound > maxAIInputTokens || r.BudgetNanodollars <= 0 || r.ReservedNanodollars != r.InputTokenUpperBound*inputTokenNanodollars+maxAIOutputTokens*outputTokenNanodollars || r.ReservedNanodollars > r.BudgetNanodollars || r.Validation.Execution != "disabled" {
		return fmt.Errorf("corrupt journal reservation or execution state")
	}
	if u := r.Usage; u != nil {
		if u.InputTokens < 0 || u.InputTokens > maxAIInputTokens || u.OutputTokens < 0 || u.OutputTokens > maxAIOutputTokens || u.CostUpperNanodollars != u.InputTokens*inputTokenNanodollars+u.OutputTokens*outputTokenNanodollars {
			return fmt.Errorf("corrupt journal usage")
		}
	}
	switch r.Status {
	case "reserved", "failed", "invalid":
	case "validated":
		if r.Recommendation == nil || r.Usage == nil || r.Usage.InputTokens > r.InputTokenUpperBound || r.Validation.Recommendation != "valid" {
			return fmt.Errorf("validated journal record has no recommendation")
		}
		if err := validateRecommendation(*r.Recommendation, r.Input); err != nil {
			return fmt.Errorf("corrupt journal recommendation: %w", err)
		}
	default:
		return fmt.Errorf("unsupported journal record status")
	}
	return nil
}

func (j FileRecommendationJournal) Save(record RecommendationRecord) (err error) {
	if strings.TrimSpace(j.Directory) == "" {
		return fmt.Errorf("journal directory is required")
	}
	if err := validateJournalRecord(record); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode journal record: %w", err)
	}
	if len(data) > maxJournalRecordBytes {
		return fmt.Errorf("journal record exceeds size limit")
	}
	dir := j.accountDirectory(record.AccountID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create journal directory: %w", err)
	}
	// MkdirAll does not tighten permissions on pre-existing directories.
	if err := os.Chmod(j.Directory, 0700); err != nil {
		return fmt.Errorf("protect journal directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("protect account journal: %w", err)
	}
	f, err := os.CreateTemp(dir, ".record-*") // 0600, same filesystem as destination.
	if err != nil {
		return fmt.Errorf("create journal temporary file: %w", err)
	}
	defer func() {
		if removeErr := os.Remove(f.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove journal temporary file: %w", removeErr))
		}
	}()
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write journal record: %w", err)
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, record.ID+".json")); err != nil {
		return fmt.Errorf("publish journal record: %w", err)
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open journal directory for sync: %w", err)
	}
	return errors.Join(d.Sync(), d.Close())
}
