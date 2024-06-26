package slogpretty

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/require"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestLogHandler_Handle(t *testing.T) {
	bufWo := bytes.NewBuffer(nil)
	bufWe := bytes.NewBuffer(nil)

	h := &Handler{
		We:  &lockedWriter{w: bufWe},
		Wo:  &lockedWriter{w: bufWo},
		Lvl: slog.LevelDebug,
		Goa: make([]GroupOrAttrs, 0),
	}

	record := slog.Record{
		Time:    time.Date(2024, 06, 26, 0, 0, 0, 0, time.UTC),
		Message: "::1",
		Level:   slog.LevelDebug,
	}
	record.Add("method", http.MethodGet)
	record.Add("status", http.StatusOK)
	record.Add("latency", 2*time.Second)
	record.Add("location", "../foo")
	record.Add(slog.Group("foo", slog.String("bar", "bar")))
	require.NoError(t, h.Handle(context.Background(), record))
	record.Level = slog.LevelInfo
	require.NoError(t, h.Handle(context.Background(), record))
	record.Level = slog.LevelWarn
	require.NoError(t, h.Handle(context.Background(), record))
	record.Level = slog.LevelError
	require.NoError(t, h.Handle(context.Background(), record))
	record.Message = "unknown"
	require.NoError(t, h.Handle(context.Background(), record))
}
