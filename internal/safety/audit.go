package safety

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

type Audit struct {
	logger  *slog.Logger
	file    *os.File
	secrets []string
}

func NewAudit(stderr io.Writer, path string, secrets ...string) (*Audit, error) {
	var file *os.File
	if path != "" {
		if info, e := os.Lstat(path); e == nil {
			if !info.Mode().IsRegular() || info.Mode().Perm() & ^os.FileMode(0600) != 0 {
				return nil, fmt.Errorf("audit file must be regular with 0600 permissions")
			}
		} else if !os.IsNotExist(e) {
			return nil, e
		}
		var e error
		file, e = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600) //nolint:gosec // Audit destination is an explicit operator setting; existing type and permissions checked above.
		if e != nil {
			return nil, e
		}
		stderr = io.MultiWriter(stderr, file)
	}
	handler := slog.NewJSONHandler(stderr, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		switch a.Key {
		case slog.TimeKey:
			a.Key = "ts"
		case slog.LevelKey, slog.MessageKey:
			return slog.Attr{}
		}
		return a
	}})
	return &Audit{logger: slog.New(handler), file: file, secrets: secrets}, nil
}
func (a *Audit) Close() error {
	if a.file != nil {
		return a.file.Close()
	}
	return nil
}
func (a *Audit) Record(tool, action, bucket, key, outcome string, gated bool, start time.Time) {
	clean := func(v string) string { return Redact(v, a.secrets...) }
	a.logger.Info("", "tool", clean(tool), "s3_action", clean(action), "bucket", clean(bucket), "key", clean(key), "outcome", clean(outcome), "gated", gated, "duration_ms", time.Since(start).Milliseconds())
}
