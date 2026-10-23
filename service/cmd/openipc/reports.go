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

	"github.com/OpenIPC/website/service/internal/boards"
	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/reports"
)

const reportsUsage = `usage: openipc reports
  list [--status pending|published|rejected|withdrawn]   the review queue, newest first (JSON), each with
                                                same_camera, same_sender and duplicate_of (the newest 100)
  show <id>                                     the report whole, as sent: YAML, identifiers' hashes, files
  publish <id> [--model <board model id>]... [--new-board] [--certainty c --evidence why] [--by who] [--note text]
                                                --new-board: add the camera the report proposed, as the review page suggests
  reject <id> [--by who] [--note text]
  link <id> <board model id> [--certainty c --evidence why] [--by who]
                                                say which board a report is from
  certainty <id> <board model id> <confirmed|probable|guess> [--evidence why]
                                                say how sure an existing link is now
  unlink <id> <board model id>                  undo a link made in error
  takedown <id> --by who --note why              withdraw for good: YAML blanked, files deleted
  verify                                        re-hash every stored file; non-zero on any missing or changed

A link is confirmed (the default: a sticker, the device ID in its firmware),
probable (the platform ipctool names, a sibling report) or a guess; one not
confirmed needs --evidence, shown beside it: "same platform as r-3yi3z6jw".
Linking again never re-grades a link: certainty does.`

// kinLimit: how many of the listed reports are given their kin -- as many
// as the review page shows.
const kinLimit = 100

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
	files := &reports.Files{Root: cfg.ReportsRoot}
	st := &reports.Store{DB: pool, Files: files}

	fs := flag.NewFlagSet("reports "+args[0], flag.ExitOnError)
	by := fs.String("by", whoami(), "who decided")
	note := fs.String("note", "", "why, for the record")
	status := fs.String("status", "", "list only reports in this state")
	var models multi
	fs.Var(&models, "model", "the board model id the report is from (repeatable)")
	newBoard := fs.Bool("new-board", false, "add the camera the report proposed to the catalogue, and link the report to it")
	certainty := fs.String("certainty", reports.Confirmed, "how sure the link is: confirmed, probable or guess")
	evidence := fs.String("evidence", "", "why the link is as sure as it is, shown beside one not confirmed")
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
		// The newest hundred with their kin, as the review page shows
		// them: the same camera's other reports, others sent from its
		// network, and the report it is a duplicate of.
		var ids []string
		for _, r := range l[:min(len(l), kinLimit)] {
			ids = append(ids, r.ID)
		}
		kin, err := st.Kin(ctx, ids)
		if err != nil {
			return err
		}
		type listed struct {
			reports.Listed
			reports.Kin
		}
		all := make([]listed, len(l))
		for i, r := range l {
			all[i].Listed = r
			if k := kin[r.ID]; k != nil {
				all[i].Kin = *k
			}
		}
		return out.Encode(all)
	case "show":
		if err := need(1); err != nil {
			return err
		}
		r, err := st.Private(ctx, pos[0])
		if err != nil {
			return err
		}
		_, facts, _ := reports.Parse(r.YAML)
		devices, err := st.DeviceIDs(ctx, r.ID)
		if err != nil {
			return err
		}
		id, err := reports.IdentifyDevices(ctx, pool, facts, devices)
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
		var nb *boards.NewModel
		if *newBoard {
			if nb, err = suggestedBoard(ctx, cfg, st, pos[0]); err != nil {
				return err
			}
		}
		basis := reports.Basis{Certainty: *certainty, Evidence: *evidence}
		d, err := st.Decide(ctx, pos[0], cmd, *by, *note, models, basis, nb, nil)
		if err != nil {
			return err
		}
		refreshReportUnits(ctx, cfg, log, pool)
		log.Info("reports: reviewed", "report", pos[0], "decision", cmd, "by", *by, "models", []string(models),
			"certainty", *certainty, "new_board", d.Board, "member", d.Member, "stars", d.Points)
		return nil
	case "link":
		if err := need(2); err != nil {
			return err
		}
		err := st.LinkAs(ctx, pos[0], pos[1], *by, reports.Basis{Certainty: *certainty, Evidence: *evidence})
		if errors.Is(err, reports.ErrLinked) {
			return fmt.Errorf("%s %s: %w; to change it: openipc reports certainty %[1]s %[2]s <confirmed|probable|guess> [--evidence why]",
				pos[0], pos[1], err)
		}
		if err != nil {
			return err
		}
		refreshReportUnits(ctx, cfg, log, pool)
		return nil
	case "certainty":
		if err := need(3); err != nil {
			return err
		}
		if err := st.SetCertainty(ctx, pos[0], pos[1], reports.Basis{Certainty: pos[2], Evidence: *evidence}); err != nil {
			return err
		}
		// The report's unit on the board says it too.
		refreshReportUnits(ctx, cfg, log, pool)
		log.Info("reports: link certainty", "report", pos[0], "model", pos[1], "certainty", pos[2], "evidence", *evidence, "by", *by)
		return nil
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

// suggestedBoard is the board the review page would add for a report's
// proposal, with the catalogue to resolve its SoC.
func suggestedBoard(ctx context.Context, cfg *config.Config, st *reports.Store, id string) (*boards.NewModel, error) {
	p, err := st.ProposalOf(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("%s proposes no new camera; link it with --model", id)
	}
	devices, err := st.DeviceIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	sug, err := boards.Suggest(ctx, st.DB, p.Maker, p.Board, p.SoC, devices...)
	if err != nil {
		return nil, err
	}
	if sug.Existing != "" {
		return nil, fmt.Errorf("the catalogue already has %s; publish with --model %s", sug.Existing, sug.Existing)
	}
	if cat, err := catalogue.Load(cfg.CatalogueDir); err == nil {
		st.SoC = socResolver(cat)
	}
	return &sug.NewModel, nil
}

// socResolver maps a SoC to the catalogue's slug, as the importers do.
func socResolver(cat *catalogue.Catalogue) func(string) string {
	return func(label string) string {
		if s := cat.SoC(label); s != nil {
			return s.URLName
		}
		return ""
	}
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
