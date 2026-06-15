package lock

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEncodeIDIsFilenameSafe(t *testing.T) {
	encoded := EncodeID("target:abc/def\\ghi")
	if strings.ContainsAny(encoded, `:/\`) {
		t.Fatalf("encoded id %q contains path separators or colon", encoded)
	}
}

func TestAcquireTargetsSortsAndDeduplicates(t *testing.T) {
	manager := NewManager(t.TempDir()).WithTimeout(time.Second)
	set, err := manager.AcquireTargets(context.Background(), []string{"target:b", "target:a", "target:a"})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()
	var ids []string
	for _, held := range set.Locks {
		ids = append(ids, held.ID)
		if filepath.Base(held.Path) == held.ID+".lock" {
			t.Fatalf("lock path used raw id: %s", held.Path)
		}
	}
	if want := []string{"target:a", "target:b"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %#v, want %#v", ids, want)
	}
}

func TestAcquireReleasesEarlierLocksOnFailure(t *testing.T) {
	dir := t.TempDir()
	manager := NewManager(dir).WithTimeout(50 * time.Millisecond)
	holder, err := manager.AcquireTargets(context.Background(), []string{"target:b"})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	if _, err := manager.AcquireTargets(context.Background(), []string{"target:a", "target:b"}); err == nil {
		t.Fatal("expected contention failure")
	}
	reacquired, err := manager.AcquireTargets(context.Background(), []string{"target:a"})
	if err != nil {
		t.Fatalf("target:a should have been released after partial failure: %v", err)
	}
	defer reacquired.Release()
}

func TestCrossProcessContentionTimesOut(t *testing.T) {
	if os.Getenv("SKILLER_LOCK_HELPER") == "1" {
		helperHoldLock(t)
		return
	}
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=TestCrossProcessContentionTimesOut")
	cmd.Env = append(os.Environ(),
		"SKILLER_LOCK_HELPER=1",
		"SKILLER_LOCK_DIR="+dir,
		"SKILLER_LOCK_ID=target:shared",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("waiting for helper lock: %v", err)
	}
	if strings.TrimSpace(line) != "locked" {
		t.Fatalf("helper output = %q, want locked", line)
	}
	manager := NewManager(dir).WithTimeout(75 * time.Millisecond)
	if _, err := manager.AcquireTargets(context.Background(), []string{"target:shared"}); err == nil {
		t.Fatal("expected cross-process lock contention")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper failed: %v", err)
	}
}

func helperHoldLock(t *testing.T) {
	t.Helper()
	manager := NewManager(os.Getenv("SKILLER_LOCK_DIR")).WithTimeout(time.Second)
	set, err := manager.AcquireTargets(context.Background(), []string{os.Getenv("SKILLER_LOCK_ID")})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()
	if _, err := os.Stdout.WriteString("locked\n"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
}
