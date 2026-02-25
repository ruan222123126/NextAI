package cron

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nextai/apps/gateway/internal/domain"
)

type leaseSlot struct {
	LeaseID string `json:"lease_id"`
	JobID   string `json:"job_id"`
	Owner   string `json:"owner"`
	Slot    int    `json:"slot"`

	AcquiredAt string `json:"acquired_at"`
	ExpiresAt  string `json:"expires_at"`
}

type leaseHandle struct {
	Path    string
	LeaseID string
}

func (s *Service) tryAcquireSlot(jobID string, runtime domain.CronRuntimeSpec) (*leaseHandle, bool, error) {
	maxConcurrency := runtime.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}

	now := time.Now().UTC()
	ttl := time.Duration(runtime.TimeoutSeconds)*time.Second + 30*time.Second
	if ttl < 30*time.Second {
		ttl = 30 * time.Second
	}

	leaseID := newLeaseID()
	dir := filepath.Join(strings.TrimSpace(s.deps.DataDir), cronLeaseDirName, encodeJobID(jobID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, err
	}

	for slot := 0; slot < maxConcurrency; slot++ {
		path := filepath.Join(dir, fmt.Sprintf("slot-%d.json", slot))
		if err := cleanupExpiredLease(path, now); err != nil {
			return nil, false, err
		}

		lease := leaseSlot{
			LeaseID:    leaseID,
			JobID:      jobID,
			Owner:      fmt.Sprintf("pid:%d", os.Getpid()),
			Slot:       slot,
			AcquiredAt: now.Format(time.RFC3339Nano),
			ExpiresAt:  now.Add(ttl).Format(time.RFC3339Nano),
		}
		body, err := json.Marshal(lease)
		if err != nil {
			return nil, false, err
		}

		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, false, err
		}
		if _, err := f.Write(body); err != nil {
			_ = f.Close()
			_ = removeIfExists(path)
			return nil, false, err
		}
		if err := f.Close(); err != nil {
			_ = removeIfExists(path)
			return nil, false, err
		}
		return &leaseHandle{Path: path, LeaseID: leaseID}, true, nil
	}
	return nil, false, nil
}

func (s *Service) releaseSlot(slot *leaseHandle) {
	if slot == nil || strings.TrimSpace(slot.Path) == "" {
		return
	}

	body, err := os.ReadFile(slot.Path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("release cron lease read failed: path=%s err=%v", slot.Path, err)
		return
	}

	var lease leaseSlot
	if err := json.Unmarshal(body, &lease); err != nil {
		if rmErr := removeIfExists(slot.Path); rmErr != nil {
			log.Printf("release cron lease cleanup failed: path=%s err=%v", slot.Path, rmErr)
		}
		return
	}
	if lease.LeaseID != slot.LeaseID {
		return
	}
	if err := os.Remove(slot.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("release cron lease failed: path=%s err=%v", slot.Path, err)
	}
}

func cleanupExpiredLease(path string, now time.Time) error {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var lease leaseSlot
	if err := json.Unmarshal(body, &lease); err != nil {
		return removeIfExists(path)
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(lease.ExpiresAt))
	if err != nil {
		return removeIfExists(path)
	}
	if !now.After(expiresAt.UTC()) {
		return nil
	}
	return removeIfExists(path)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func encodeJobID(jobID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(jobID))
}

func newLeaseID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%x", os.Getpid(), buf)
}
