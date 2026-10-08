package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/crashes"
)

const crashesUsage = `usage: openipc crashes
  list                                          every signature, worst first, bogus and merged too (JSON)
  show <signature>                              where it was seen, on which builds, and its crashes' redacted logs (JSON)
  status <signature> open|confirmed|fixed|wontfix|bogus [--fixed-in v] [--issue url] [--merge-into sig] [--by who] [--note text]
                                                triage; bogus takes back every star it paid
  settle                                        pay what crashes earned (also run by purge)
  takedown <crash id>                           delete one crash and, when nothing else came in it, its bundle`

// crashesCommand is `openipc crashes ...`, run in the web container; the
// same triage is on /club for maintainers.
func crashesCommand(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	if len(args) == 0 {
		return errors.New(crashesUsage)
	}
	pool, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := &crashes.Store{DB: pool}

	fs := flag.NewFlagSet("crashes "+args[0], flag.ExitOnError)
	by := fs.String("by", whoami(), "who decided")
	note := fs.String("note", "", "why, for the record")
	fixedIn := fs.String("fixed-in", "", "the firmware version the fix is in")
	issue := fs.String("issue", "", "the issue tracking it (https://)")
	mergeInto := fs.String("merge-into", "", "the signature this one is the same bug as")
	pos, err := parseInterleaved(fs, args[1:])
	if err != nil {
		return err
	}
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	now := time.Now()
	switch args[0] {
	case "list":
		list, err := st.Ranked(ctx, true, now)
		if err != nil {
			return err
		}
		return out.Encode(list)
	case "show":
		if len(pos) != 1 {
			return errors.New(crashesUsage)
		}
		g, err := st.Get(ctx, pos[0], true, now)
		if err != nil {
			return err
		}
		combos, err := st.Combos(ctx, pos[0], true)
		if err != nil {
			return err
		}
		details, err := st.Details(ctx, pos[0])
		if err != nil {
			return err
		}
		return out.Encode(map[string]any{"signature": g, "seen_on": combos, "crashes": details})
	case "status":
		if len(pos) != 2 {
			return errors.New(crashesUsage)
		}
		taken, err := st.Decide(ctx, pos[0], *by, crashes.Triage{Status: pos[1], FixedIn: *fixedIn, IssueURL: *issue,
			MergeInto: *mergeInto, Note: *note})
		if err != nil {
			return err
		}
		log.Info("crashes: triaged", "signature", pos[0], "status", pos[1], "by", *by, "stars_taken_back", taken)
		return nil
	case "settle":
		return settleCrashes(ctx, log, pool)
	case "takedown":
		if len(pos) != 1 {
			return errors.New(crashesUsage)
		}
		if err := st.Takedown(ctx, pos[0]); err != nil {
			return err
		}
		log.Info("crashes: taken down", "id", pos[0], "by", *by)
		return nil
	}
	return errors.New(crashesUsage)
}

// settleCrashes is the nightly settlement of the crashes' stars.
func settleCrashes(ctx context.Context, log *slog.Logger, pool *pgxpool.Pool) error {
	res, err := (&crashes.Store{DB: pool}).Settle(ctx, time.Now())
	if err != nil {
		return err
	}
	log.Info("crashes: settled", "members", res.Members, "stars", res.Awarded, "held_over_cap", res.Held)
	return nil
}
