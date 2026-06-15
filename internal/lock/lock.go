package lock

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gofrs/flock"
)

const (
	defaultTimeout    = 5 * time.Second
	defaultRetryDelay = 10 * time.Millisecond
)

type Manager struct {
	StateDir   string
	Timeout    time.Duration
	RetryDelay time.Duration
}

type Held struct {
	ID   string
	Path string

	lock *flock.Flock
}

type Set struct {
	Locks []Held
}

func NewManager(stateDir string) Manager {
	return Manager{
		StateDir:   stateDir,
		Timeout:    defaultTimeout,
		RetryDelay: defaultRetryDelay,
	}
}

func (m Manager) WithTimeout(timeout time.Duration) Manager {
	m.Timeout = timeout
	return m
}

func (m Manager) AcquireTargets(ctx context.Context, ids []string) (*Set, error) {
	return m.acquire(ctx, ids)
}

func (m Manager) AcquireState(ctx context.Context) (*Set, error) {
	return m.acquire(ctx, []string{"state:state.json"})
}

func (m Manager) acquire(ctx context.Context, ids []string) (*Set, error) {
	ids = SortedUnique(ids)
	set := &Set{Locks: make([]Held, 0, len(ids))}
	if len(ids) == 0 {
		return set, nil
	}
	if m.StateDir == "" {
		return nil, errors.New("state dir is required for locks")
	}
	timeout := m.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	retryDelay := m.RetryDelay
	if retryDelay <= 0 {
		retryDelay = defaultRetryDelay
	}
	if err := os.MkdirAll(filepath.Join(m.StateDir, "locks"), 0o755); err != nil {
		return nil, fmt.Errorf("create locks dir: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for _, id := range ids {
		path := m.Path(id)
		f := flock.New(path)
		ok, err := f.TryLockContext(ctx, retryDelay)
		if err != nil {
			_ = set.Release()
			return nil, fmt.Errorf("lock %s: %w", id, err)
		}
		if !ok {
			_ = set.Release()
			return nil, fmt.Errorf("another skiller run holds lock %s", id)
		}
		set.Locks = append(set.Locks, Held{ID: id, Path: path, lock: f})
	}
	return set, nil
}

func (m Manager) Path(id string) string {
	return filepath.Join(m.StateDir, "locks", EncodeID(id)+".lock")
}

func (s *Set) Release() error {
	if s == nil {
		return nil
	}
	var errs []error
	for i := len(s.Locks) - 1; i >= 0; i-- {
		held := &s.Locks[i]
		if held.lock == nil {
			continue
		}
		if err := held.lock.Unlock(); err != nil {
			errs = append(errs, fmt.Errorf("unlock %s: %w", held.ID, err))
		}
		held.lock = nil
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func SortedUnique(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func EncodeID(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}
