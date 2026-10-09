package symbolize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/crashes"
)

// Worker symbolizes the dumps that wait, one at a time: woken by an upload's
// NOTIFY, and every few minutes for the ones waiting to be tried again.
type Worker struct {
	Pool       *pgxpool.Pool
	Log        *slog.Logger
	Symbolizer *Symbolizer
	Now        func() time.Time
	// Every is how often it looks without being woken.
	Every time.Duration
	// KeepFor is how long symbols and rootfs images nothing has used stay.
	KeepFor time.Duration
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Run works until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	every := w.Every
	if every == 0 {
		every = 10 * time.Minute
	}
	wake := make(chan struct{}, 1)
	go w.listen(ctx, wake)
	t := time.NewTicker(every)
	defer t.Stop()
	swept := time.Time{}
	for {
		w.Drain(ctx)
		if keep := w.KeepFor; keep > 0 && time.Since(swept) > 24*time.Hour {
			if n := Sweep(w.Symbolizer.Symbols.Root, keep); n > 0 {
				w.Log.Info("crashes: swept unused symbols", "removed", n)
			}
			swept = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-t.C:
		}
	}
}

func (w *Worker) listen(ctx context.Context, wake chan<- struct{}) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := func() error {
			conn, err := w.Pool.Acquire(ctx)
			if err != nil {
				return err
			}
			defer conn.Release()
			if _, err := conn.Exec(ctx, "LISTEN "+crashes.SymbolizeChannel); err != nil {
				return err
			}
			backoff = time.Second
			for {
				if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
					return err
				}
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}()
		if ctx.Err() != nil {
			return
		}
		w.Log.Warn("crashes: symbolize notifications lost, reconnecting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
		// What arrived while it was down is found by the next drain.
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

// Drain symbolizes every dump whose turn it is.
func (w *Worker) Drain(ctx context.Context) {
	st := &crashes.Store{DB: w.Pool}
	for ctx.Err() == nil {
		due, err := st.DueDumps(ctx, w.now(), 8)
		if err != nil {
			w.Log.Error("crashes: dumps to symbolize", "err", err)
			return
		}
		if len(due) == 0 {
			return
		}
		for _, d := range due {
			// What cannot be stored is left for the next tick, not tried
			// again at once: the same dump would only fail the same way.
			if !w.one(ctx, st, d) {
				return
			}
		}
	}
}

// symbolize runs the symbolizer, a panic in it -- the dumps come from
// anyone -- being one more failure of that dump's.
func (w *Worker) symbolize(ctx context.Context, d crashes.Due) (res *Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			res, err = nil, fmt.Errorf("symbolizer panicked: %v", p)
		}
	}()
	return w.Symbolizer.Symbolize(ctx, d.Dump, firmwareOf(d.Meta), crashes.LastTry(d.Attempts))
}

// one symbolizes a dump, and says whether what came of it was stored.
func (w *Worker) one(ctx context.Context, st *crashes.Store, d crashes.Due) bool {
	res, err := w.symbolize(ctx, d)
	if err == nil {
		sig, serr := st.Symbolized(ctx, d.EventID, res.Frames, res.Sources, w.now())
		if serr == nil {
			w.Log.Info("crashes: symbolized", "id", d.EventID, "signature", sig, "frames", len(res.Frames))
			return true
		}
		w.Log.Error("crashes: symbolized, not stored", "id", d.EventID, "err", serr)
		// Counted as a failed try, so a backtrace that cannot be stored is
		// given up on like any other.
		err = serr
	}
	if ctx.Err() != nil {
		return false
	}
	gaveUp, ferr := st.SymbolizeFailed(ctx, d.EventID, err.Error(), w.now())
	if ferr != nil {
		w.Log.Error("crashes: symbolize failure not stored", "id", d.EventID, "err", ferr)
		return false
	}
	level := slog.LevelWarn
	if errors.Is(err, ErrNotPublished) {
		level = slog.LevelInfo
	}
	w.Log.Log(ctx, level, "crashes: not symbolized", "id", d.EventID, "attempt", d.Attempts+1, "gave_up", gaveUp, "err", err)
	return true
}

// firmwareOf is the build meta.json names.
func firmwareOf(meta json.RawMessage) Firmware {
	var m struct {
		Firmware struct {
			BuildID  string `json:"build_id"`
			Platform string `json:"platform"`
		} `json:"firmware"`
	}
	if len(meta) == 0 || json.Unmarshal(meta, &m) != nil {
		return Firmware{}
	}
	return Firmware{Build: m.Firmware.BuildID, Platform: m.Firmware.Platform}
}
