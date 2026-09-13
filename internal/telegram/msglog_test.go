package telegram

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestMessageLog(t *testing.T) *MessageLog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "acl.sqlite")
	log, err := OpenMessageLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return log
}

func TestMessageLogRecordExpiredRemove(t *testing.T) {
	log := openTestMessageLog(t)

	now := time.Now()
	if err := log.Record(100, 1, now.Add(-49*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := log.Record(100, 2, now.Add(-1*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := log.Record(200, 3, now.Add(-50*time.Hour)); err != nil {
		t.Fatal(err)
	}

	expired, err := log.Expired(now.Add(-48 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 2 {
		t.Fatalf("expired count = %d, want 2: %+v", len(expired), expired)
	}

	if err := log.Remove(100, 1); err != nil {
		t.Fatal(err)
	}
	expired, err = log.Expired(now.Add(-48 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0].MessageID != 3 {
		t.Fatalf("after remove expired = %+v", expired)
	}
}

func TestMessageLogRecordIgnoreDuplicate(t *testing.T) {
	log := openTestMessageLog(t)

	first := time.Now().Add(-10 * time.Hour)
	second := time.Now()
	if err := log.Record(100, 1, first); err != nil {
		t.Fatal(err)
	}
	if err := log.Record(100, 1, second); err != nil {
		t.Fatal(err)
	}

	// If the duplicate overwrote sent_at to "now", this cutoff would miss the row.
	expired, err := log.Expired(time.Now().Add(-1 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0].MessageID != 1 {
		t.Fatalf("duplicate record should keep original sent_at, got expired %+v", expired)
	}
}

func TestIsDeleteGoneError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errString("Bad Request: message to delete not found"), true},
		{errString("Bad Request: message can't be deleted for everyone"), true},
		{errString("Too Many Requests"), false},
	}
	for _, tc := range cases {
		if got := isDeleteGoneError(tc.err); got != tc.want {
			t.Fatalf("isDeleteGoneError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
