package fox

import (
	netcontext "context"
	"io"
	"log/slog"
	"testing"
)

func BenchmarkSlogHandler_Handle(b *testing.B) {
	log := slog.New(NewSlogHandler(io.Discard))
	ctx := netcontext.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		log.LogAttrs(ctx, slog.LevelInfo, "foo bar", slog.String("foo", "bar"), colorizedAttr("damn", "bar", bgRedColor))
	}
}
