package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/user"

	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/reports"
)

const reportsUsage = `usage: openipc reports
  list [--status pending|published|rejected|withdrawn]   the review queue, newest first (JSON)
  show <id>                                     the report whole, as sent: YAML, identifiers' hashes, files
  publish <id> [--model <board model id>]... [--by who] [--note text]
  reject <id> [--by who] [--note text]
  link <id> <board model id> [--by who]         say which board a report is from
  unlink <id> <board model id>                  undo a link made in error
  takedown <id> --by who --note why              withdraw for good: YAML blanked, files deleted
  verify                                        re-hash every stored file; non-zero on any missing or changed`

// reportsCommand is `openipc reports ...`: the review queue for owner
// reports. There is no admin page (#288); a maintainer runs these in the web
// container.
func reportsCommand(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	if len(args) == 0 {
		return errors.New(reportsUsage)
	}
	pool, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := &reports.Store{DB: pool}
	files := &reports.Files{Root: cfg.ReportsRoot}

	fs := flag.NewFlagSet("reports "+args[0], flag.ExitOnError)
	by := fs.String("by", whoami(), "who decided")
	note := fs.String("note", "", "why, for the record")
	status := fs.String("status", "", "list only reports in this state")
	var models multi
	fs.Var(&models, "model", "the board model id the report is from (repeatable)")
	cmd := args[0]
	pos, err := parseInterleaved(fs, args[1:])
	if err != nil {
		return err
	}
	need := func(n int) error {
		if len(pos) != n {
			return errors.New(reportsUsage)
		}
		return nil
	}
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")

	switch cmd {
	case "list":
		l, err := st.List(ctx, *status)
		if err != nil {
			return err
		}
		return out.Encode(l)
	case "show":
		if err := need(1); err != nil {
			return err
		}
		r, err := st.Private(ctx, pos[0])
		if err != nil {
			return err
		}
		_, facts, _ := reports.Parse(r.YAML)
		id, err := reports.Identify(ctx, pool, facts)
		if err != nil {
			return err
		}
		fmt.Printf("# %s  %s  %s  backup: %s\n# tool: %s\n# note: %s\n# identifiers (keyed): %v\n",
			r.ID, r.ReceivedAt.Format("2006-01-02 15:04 MST"), r.Channel, r.Consent, r.Tool, r.Note, r.IDHashes)
		for _, f := range r.Files {
			served := "private"
			if f.PublicSHA256 != "" {
				served = "public " + f.PublicSHA256[:12]
			}
			fmt.Printf("# file %d: %s %s %d bytes sha256 %s (%s)\n", f.Position, f.Kind, f.Name, f.Bytes, f.SHA256[:12], served)
		}
		fmt.Println(r.YAML)
		fmt.Println("# identify:")
		return out.Encode(id)
	case "publish", "reject":
		if err := need(1); err != nil {
			return err
		}
		// The same decision the club's review page makes: links, the
		// review, the sender's stars, and the boards' copy of the text.
		d, err := st.Decide(ctx, pos[0], cmd, *by, *note, models)
		if err != nil {
			return err
		}
		refreshReportUnits(ctx, cfg, log, pool)
		log.Info("reports: reviewed", "report", pos[0], "decision", cmd, "by", *by, "models", []string(models),
			"member", d.Member, "stars", d.Points)
		return nil
	case "link":
		if err := need(2); err != nil {
			return err
		}
		return st.Link(ctx, pos[0], pos[1], *by)
	case "unlink":
		if err := need(2); err != nil {
			return err
		}
		return st.Unlink(ctx, pos[0], pos[1])
	case "takedown":
		if err := need(1); err != nil {
			return err
		}
		if *note == "" {
			return errors.New("takedown needs --note: who asked, and why")
		}
		orphans, err := st.Takedown(ctx, pos[0], *by, *note)
		if err != nil {
			return err
		}
		deleted := 0
		for _, sum := range orphans {
			ok, err := st.RemoveUnreferenced(ctx, files, sum)
			if err != nil {
				return err
			}
			if ok {
				deleted++
			}
		}
		log.Info("reports: taken down", "report", pos[0], "by", *by, "files_deleted", deleted)
		return nil
	case "verify":
		checked, bad, err := reports.Verify(ctx, st, files)
		if err != nil {
			return err
		}
		log.Info("reports: verify", "files", checked, "bad", len(bad))
		if len(bad) > 0 {
			return fmt.Errorf("%d of %d report files are missing or changed: %v", len(bad), checked, bad)
		}
		return nil
	}
	return errors.New(reportsUsage)
}

// parseInterleaved lets flags follow the positional arguments
// (`publish r-abc --model x`), which flag.Parse alone stops at.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

type multi []string

func (m *multi) String() string     { return fmt.Sprint([]string(*m)) }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func whoami() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "maintainer"
}
