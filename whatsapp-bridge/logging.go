package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// Logging rules (see CLAUDE.md): log who, where, when, message type and
// message ID only. NEVER log message text, captions or file names.
//
// Logs go to logs/bridge-YYYY-MM-DD.log (one file per day) and are deleted
// after logKeepDays days. The QR code is never written to a log file.

const (
	logDir      = "logs"
	logKeepDays = 14
	logPrefix   = "bridge-"
	logSuffix   = ".log"
)

type dailyLog struct {
	mu   sync.Mutex
	dir  string
	day  string
	file *os.File
}

var appLogWriter = &dailyLog{dir: logDir}

func (d *dailyLog) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	today := time.Now().Format("2006-01-02")
	if d.file == nil || d.day != today {
		if d.file != nil {
			d.file.Close()
			d.file = nil
		}
		if err := os.MkdirAll(d.dir, 0o700); err != nil {
			return 0, err
		}
		f, err := os.OpenFile(filepath.Join(d.dir, logPrefix+today+logSuffix), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return 0, err
		}
		d.file, d.day = f, today
		go pruneOldLogs(d.dir, logKeepDays)
	}
	return d.file.Write(p)
}

// pruneOldLogs deletes bridge-YYYY-MM-DD.log files older than keepDays.
func pruneOldLogs(dir string, keepDays int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -keepDays)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, logPrefix) || !strings.HasSuffix(name, logSuffix) {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(strings.TrimPrefix(name, logPrefix), logSuffix), time.Local)
		if err != nil {
			continue
		}
		if day.Before(cutoff) {
			os.Remove(filepath.Join(dir, name))
		}
	}
}

// appLog writes one timestamped line to the log file and the terminal.
// Callers must only pass identifiers, never message text or file names.
func appLog(format string, args ...interface{}) {
	line := fmt.Sprintf("%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	io.WriteString(appLogWriter, line)
	io.WriteString(os.Stdout, line)
}

// safeLogger plugs into whatsmeow so its own messages also go to the daily
// log. Debug output is dropped because it can contain message content.
type safeLogger struct{ module string }

func newSafeLogger(module string) waLog.Logger { return &safeLogger{module: module} }

func (l *safeLogger) out(level, msg string, args ...interface{}) {
	appLog("[%s %s] %s", l.module, level, fmt.Sprintf(msg, args...))
}
func (l *safeLogger) Debugf(string, ...interface{})       {}
func (l *safeLogger) Infof(msg string, a ...interface{})  { l.out("INFO", msg, a...) }
func (l *safeLogger) Warnf(msg string, a ...interface{})  { l.out("WARN", msg, a...) }
func (l *safeLogger) Errorf(msg string, a ...interface{}) { l.out("ERROR", msg, a...) }
func (l *safeLogger) Sub(module string) waLog.Logger {
	return &safeLogger{module: l.module + "/" + module}
}
