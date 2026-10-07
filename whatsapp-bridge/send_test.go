package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
)

const (
	ownNumber   = "15550001111"
	otherNumber = "15552223333"
)

// newTestSender builds a Sender with a fake WhatsApp connection inside a
// temporary directory, so nothing real is ever touched or sent.
func newTestSender(t *testing.T) (*Sender, *int, *time.Time) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewMessageStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	sent := 0
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := &Sender{
		store:     store,
		now:       func() time.Time { return clock },
		ownUsers:  func() []string { return []string{ownNumber} },
		connected: func() bool { return true },
		transmit: func(ctx context.Context, to types.JID, msg *waProto.Message) (string, time.Time, error) {
			sent++
			return "ID" + strings.Repeat("X", sent), clock, nil
		},
		upload: func(ctx context.Context, data []byte, t whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
			return whatsmeow.UploadResponse{URL: "u", DirectPath: "d", MediaKey: []byte{1}, FileLength: uint64(len(data))}, nil
		},
		chatName: func(jid types.JID) string { return jid.User },
	}
	return s, &sent, &clock
}

func setTestLock(t *testing.T, on bool) {
	t.Helper()
	p := filepath.Join(configDir, testLockFile)
	if on {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		os.Remove(p)
	}
}

func TestTestLock(t *testing.T) {
	s, sent, _ := newTestSender(t)

	setTestLock(t, true)
	if r := s.Send(otherNumber, "hi", "", false); r.Code != http.StatusForbidden || *sent != 0 {
		t.Fatalf("test lock should block others, got code=%d sent=%d", r.Code, *sent)
	}
	if r := s.Send(otherNumber+"@g.us", "hi", "", false); r.Code != http.StatusForbidden {
		t.Fatalf("test lock should block groups, got %d", r.Code)
	}
	// A group whose ID happens to equal our number must not count as "own".
	if r := s.Send(ownNumber+"@g.us", "hi", "", false); r.Code != http.StatusForbidden {
		t.Fatalf("group with own number must not pass, got %d", r.Code)
	}
	if r := s.Send("+"+ownNumber, "to me", "", false); !r.Success || *sent != 1 {
		t.Fatalf("test lock should allow own number, got %+v", r)
	}
	if r := s.Send(ownNumber+"@s.whatsapp.net", "to me again", "", false); !r.Success {
		t.Fatalf("own JID should pass, got %+v", r)
	}

	// Switched off without any restart.
	setTestLock(t, false)
	if r := s.Send(otherNumber, "hi", "", false); !r.Success {
		t.Fatalf("lock off should allow others, got %+v", r)
	}
	// Switched back on, immediately effective.
	setTestLock(t, true)
	if r := s.Send(otherNumber, "different text", "", false); r.Code != http.StatusForbidden {
		t.Fatalf("lock back on should block, got %d", r.Code)
	}
}

func TestRepeatLock(t *testing.T) {
	s, sent, clock := newTestSender(t)

	if r := s.Send(otherNumber, "hello", "", false); !r.Success {
		t.Fatal(r.Message)
	}
	if r := s.Send(otherNumber, "hello", "", false); r.Code != http.StatusConflict || *sent != 1 {
		t.Fatalf("same text should be refused, got code=%d sent=%d", r.Code, *sent)
	}
	if r := s.Send(otherNumber, "  hello  ", "", false); r.Code != http.StatusConflict {
		t.Fatalf("whitespace-only change should still be refused, got %d", r.Code)
	}
	if r := s.Send("+"+otherNumber+"@s.whatsapp.net", "hello", "", false); r.Code != http.StatusConflict {
		t.Fatalf("same person written differently should be refused, got %d", r.Code)
	}
	if r := s.Send(otherNumber, "hello there", "", false); !r.Success {
		t.Fatalf("different text must be allowed, got %+v", r)
	}
	if r := s.Send("15554445555", "hello", "", false); !r.Success {
		t.Fatalf("same text to someone else must be allowed, got %+v", r)
	}
	if r := s.Send(otherNumber, "hello", "", true); !r.Success {
		t.Fatalf("deliberate resend must be allowed, got %+v", r)
	}
	*clock = clock.Add(25 * time.Hour)
	if r := s.Send(otherNumber, "hello", "", false); !r.Success {
		t.Fatalf("after 24h resend must be allowed, got %+v", r)
	}
}

func TestSendRecord(t *testing.T) {
	s, _, _ := newTestSender(t)
	f := filepath.Join(t.TempDir(), "report.pdf")
	os.WriteFile(f, []byte("pdf-bytes"), 0o644)

	if r := s.Send(otherNumber, "see attached", f, false); !r.Success {
		t.Fatal(r.Message)
	}
	if r := s.Send(otherNumber, "see attached", f, false); r.Code != http.StatusConflict {
		t.Fatalf("same file+caption should be refused, got %d", r.Code)
	}
	if r := s.Send(otherNumber, "plain text", "", false); !r.Success {
		t.Fatal(r.Message)
	}

	var n int
	s.store.db.QueryRow("SELECT COUNT(*) FROM sent_log WHERE chat_jid = ?", otherNumber+"@s.whatsapp.net").Scan(&n)
	if n != 2 {
		t.Fatalf("expected 2 send records, got %d", n)
	}
	// Sent messages must appear when the chat is read back.
	var content, mediaType string
	err := s.store.db.QueryRow("SELECT content, COALESCE(media_type,'') FROM messages WHERE chat_jid = ? AND media_type = 'document'", otherNumber+"@s.whatsapp.net").Scan(&content, &mediaType)
	if err != nil || content != "see attached" {
		t.Fatalf("sent file missing from messages: %v %q", err, content)
	}
	var isFromMe bool
	s.store.db.QueryRow("SELECT is_from_me FROM messages WHERE content = 'plain text'").Scan(&isFromMe)
	if !isFromMe {
		t.Fatal("sent message should be marked is_from_me")
	}
}

func TestBadRecipient(t *testing.T) {
	s, sent, _ := newTestSender(t)
	for _, bad := range []string{"abc", "12", "../../etc", "x@@y"} {
		if r := s.Send(bad, "hi", "", false); r.Code != http.StatusBadRequest || *sent != 0 {
			t.Errorf("recipient %q should be rejected, got %d", bad, r.Code)
		}
	}
}

func TestLogPruning(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().AddDate(0, 0, -20).Format("2006-01-02")
	recent := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	for _, d := range []string{old, recent} {
		os.WriteFile(filepath.Join(dir, logPrefix+d+logSuffix), []byte("x"), 0o600)
	}
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0o600)
	pruneOldLogs(dir, logKeepDays)
	if _, err := os.Stat(filepath.Join(dir, logPrefix+old+logSuffix)); !os.IsNotExist(err) {
		t.Error("20-day-old log should be deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, logPrefix+recent+logSuffix)); err != nil {
		t.Error("3-day-old log should be kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "other.txt")); err != nil {
		t.Error("unrelated files must never be deleted")
	}
}

func TestGuardRejectsBrowserStyleRequests(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
	h := guard(ok, true)
	do := func(host, ctype string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader("{}"))
		req.Host = host
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		w := httptest.NewRecorder()
		h(w, req)
		return w.Code
	}
	if c := do("127.0.0.1:8080", "application/json"); c != 200 {
		t.Errorf("normal local call should pass, got %d", c)
	}
	if c := do("127.0.0.1:8080", "text/plain"); c != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain (cross-site form trick) should be refused, got %d", c)
	}
	if c := do("evil.example.com:8080", "application/json"); c != http.StatusForbidden {
		t.Errorf("foreign Host header (DNS rebinding) should be refused, got %d", c)
	}
}
