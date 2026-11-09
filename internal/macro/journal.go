package macro

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

// Checkpoint statuses.
const (
	StatusCompleted   = "completed"
	StatusSkipped     = "skipped"
	StatusFailed      = "failed"
	StatusCompensated = "compensated"
)

// Checkpoint is one append-only JSONL record. The latest record for a step id
// wins when a run resumes.
type Checkpoint struct {
	RunID          string                `json:"run_id"`
	Macro          string                `json:"macro"`
	StepID         int                   `json:"step_id"`
	Action         string                `json:"action"`
	Status         string                `json:"status"`
	At             string                `json:"at"`
	Attempts       int                   `json:"attempts,omitempty"`
	Effect         *protocol.MacroEffect `json:"effect,omitempty"`
	IdempotencyKey string                `json:"idempotency_key,omitempty"`
	Reason         string                `json:"reason,omitempty"`
	Classification string                `json:"classification,omitempty"`
	CompensationOf int                   `json:"compensation_of,omitempty"`
}

// DeadLetter captures the terminal failure of a run.
type DeadLetter struct {
	RunID          string `json:"run_id"`
	Macro          string `json:"macro"`
	StepID         int    `json:"step_id"`
	Action         string `json:"action"`
	Classification string `json:"classification"`
	Reason         string `json:"reason"`
	Attempts       int    `json:"attempts"`
	CircuitOpen    bool   `json:"circuitOpen,omitempty"`
	Snapshot       string `json:"snapshot,omitempty"`
	Compensated    []int  `json:"compensated,omitempty"`
	At             string `json:"at"`
}

// Journal appends checkpoints and loads the completed prefix of a run.
type Journal interface {
	Append(Checkpoint) error
	Completed(runID string) ([]Checkpoint, error)
}

// DeadLetterWriter is optional; FileJournal implements it.
type DeadLetterWriter interface {
	WriteDeadLetter(DeadLetter) (string, error)
}

// FileJournal stores one JSONL file and one dead-letter file per run.
type FileJournal struct {
	dir string
	mu  sync.Mutex
}

func NewFileJournal(dir string) *FileJournal {
	return &FileJournal{dir: dir}
}

func (j *FileJournal) checkpointPath(runID string) string {
	return filepath.Join(j.dir, runID+".jsonl")
}

func (j *FileJournal) deadLetterPath(runID string) string {
	return filepath.Join(j.dir, runID+".deadletter.json")
}

func (j *FileJournal) Append(record Checkpoint) error {
	if !ValidName(record.RunID) {
		return fmt.Errorf("invalid run id %q", record.RunID)
	}
	if err := os.MkdirAll(j.dir, 0o700); err != nil {
		return fmt.Errorf("create checkpoint directory: %w", err)
	}
	if record.At == "" {
		record.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	data = append(data, '\n')

	j.mu.Lock()
	defer j.mu.Unlock()
	file, err := os.OpenFile(j.checkpointPath(record.RunID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open checkpoint: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("append checkpoint: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync checkpoint: %w", err)
	}
	return file.Close()
}

// Completed returns the latest record per step id. Only completed and skipped
// steps are resumable.
func (j *FileJournal) Completed(runID string) ([]Checkpoint, error) {
	if !ValidName(runID) {
		return nil, fmt.Errorf("invalid run id %q", runID)
	}
	file, err := os.Open(j.checkpointPath(runID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	type entry struct {
		record  Checkpoint
		ordinal int
	}
	latest := map[int]entry{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	ordinal := 0
	for scanner.Scan() {
		var record Checkpoint
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if record.Status != StatusCompleted && record.Status != StatusSkipped {
			continue
		}
		ordinal++
		latest[record.StepID] = entry{record: record, ordinal: ordinal}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	records := make([]Checkpoint, 0, len(latest))
	for _, value := range latest {
		records = append(records, value.record)
	}
	// Stable order by step id keeps compensation deterministic.
	for i := 1; i < len(records); i++ {
		for k := i; k > 0 && records[k].StepID < records[k-1].StepID; k-- {
			records[k], records[k-1] = records[k-1], records[k]
		}
	}
	return records, nil
}

func (j *FileJournal) WriteDeadLetter(record DeadLetter) (string, error) {
	if !ValidName(record.RunID) {
		return "", fmt.Errorf("invalid run id %q", record.RunID)
	}
	if err := os.MkdirAll(j.dir, 0o700); err != nil {
		return "", fmt.Errorf("create checkpoint directory: %w", err)
	}
	if record.At == "" {
		record.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode dead letter: %w", err)
	}
	data = append(data, '\n')

	path := j.deadLetterPath(record.RunID)
	tmp, err := os.CreateTemp(j.dir, ".deadletter-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return path, nil
}
