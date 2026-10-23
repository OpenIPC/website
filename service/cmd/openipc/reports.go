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
	"strconv"
	"strings"

	"github.com/OpenIPC/website/service/internal/boards"
	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/reports"
)

const reportsUsage = `usage: openipc reports
  list [--status pending|asked|published|rejected|withdrawn]   the review queue, newest first (JSON), each with
                                                same_camera, same_sender and duplicate_of (the newest 100)
  show <id>                                     the report whole, as sent: YAML, identifiers' hashes, files
  publish <id> [--model <board model id>]... [--new-board] [--alias <marking>]... [--certainty c --evidence why] [--by who] [--note text]
                                                --new-board: add the camera the report proposed, as the review page suggests,
                                                  corrected by --maker <name>, --marking <as printed>, --model-id <id>,
                                                  --soc <SoC> and --kind board|camera|recorder|doorbell|base_station
                                                --alias: a further marking as printed (repeatable): the new board's,
                                                  or else the one --model board's
  reject <id> [--by who] [--note text]
  ask <id> --note question [--by who]           ask the sender instead of deciding: a member is told (bot or mail),
                                                a report sent signed out shows it on its receipt; their edit answers
  link <id> <board model id> [--certainty c --evidence why] [--by who]
                                                say which board a report is from
  certainty <id> <board model id> <confirmed|probable|guess> [--evidence why]
                                                say how sure an existing link is now
  photo <id> <position> blur|private|as_sent [--region x,y,w,h]...
                                                what a photo publishes: a copy with the regions blurred
                                                (fractions of the photo, upright), nothing (not the
                                                sender's unit), or the photo as sent
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
	var regions multi
	fs.Var(&regions, "region", "with photo blur: x,y,w,h of a region to blur, as fractions of the photo (repeatable)")
	newBoard := fs.Bool("new-board", false, "add the camera the report proposed to the catalogue, and link the report to it")
	certainty := fs.String("certainty", reports.Confirmed, "how sure the link is: confirmed, probable or guess")
	evidence := fs.String("evidence", "", "why the link is as sure as it is, shown beside one not confirmed")
	var fix boardFix
	fs.StringVar(&fix.maker, "maker", "", "with --new-board: the maker's name, for the proposal's")
	fs.StringVar(&fix.marking, "marking", "", "with --new-board: the marking as printed, for the proposal's")
	fs.StringVar(&fix.modelID, "model-id", "", "with --new-board: the new board's id, for the one suggested")
	fs.StringVar(&fix.soc, "soc", "", "with --new-board: the SoC, for the proposal's")
	fs.StringVar(&fix.kind, "kind", "", "with --new-board: "+strings.Join(boards.Kinds, ", ")+" (default board)")
	var aliases multi
	fs.Var(&aliases, "alias", "a further marking as printed the board answers to (repeatable)")
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
		markings := []string(aliases)
		if *newBoard {
			if nb, err = suggestedBoard(ctx, cfg, st, pos[0], fix); err != nil {
				return err
			}
			nb.Aliases, markings = markings, nil
		} else if fix != (boardFix{}) {
			return errors.New("--maker, --marking, --model-id, --soc and --kind describe a --new-board")
		}
		basis := reports.Basis{Certainty: *certainty, Evidence: *evidence}
		d, err := st.Decide(ctx, pos[0], cmd, *by, *note, models, basis, nb, markings, nil)
		if err != nil {
			return err
		}
		refreshReportUnits(ctx, cfg, log, pool)
		log.Info("reports: reviewed", "report", pos[0], "decision", cmd, "by", *by, "models", []string(models),
			"certainty", *certainty, "new_board", d.Board, "member", d.Member, "stars", d.Points)
		return nil
	case "ask":
		if err := need(1); err != nil {
			return err
		}
		if *note == "" {
			return errors.New("ask needs --note: the question to the sender")
		}
		res, err := askClub(cfg, log, pool).Ask(ctx, pos[0], *by, *note, nil)
		if err != nil {
			return err
		}
		switch {
		case res.Member == "":
			log.Info("reports: asked; sent signed out, so the question is on its receipt", "report", pos[0])
		case res.Quiet:
			log.Info("reports: asked; the sender muted notices, and their page on /club shows the question", "report", pos[0], "member", res.Member)
		case res.Told == "":
			log.Warn("reports: asked, but nothing could be sent to the sender; their page on /club shows the question", "report", pos[0], "member", res.Member)
		default:
			log.Info("reports: asked", "report", pos[0], "member", res.Member, "told", res.Told)
		}
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
	case "photo":
		if err := need(3); err != nil {
			return err
		}
		position, err := strconv.Atoi(pos[1])
		if err != nil {
			return fmt.Errorf("photo: the file's position is a number, not %q", pos[1])
		}
		var rs []reports.Region
		for _, r := range regions {
			var x reports.Region
			if _, err := fmt.Sscanf(r, "%g,%g,%g,%g", &x.X, &x.Y, &x.W, &x.H); err != nil {
				return fmt.Errorf("--region %q: x,y,w,h as fractions of the photo, e.g. 0.42,0.61,0.2,0.08", r)
			}
			rs = append(rs, x)
		}
		api := &reports.API{DB: pool, Files: files, Log: log}
		if err := api.DecidePhoto(ctx, pos[0], position, pos[2], rs, *by); err != nil {
			return err
		}
		// A published report's boards show the photo as decided.
		refreshReportUnits(ctx, cfg, log, pool)
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

// boardFix is what a reviewer corrects in a proposal from the command
// line, as the review page's fields do: a typo in the marking, the maker's
// proper name, a camera rather than a board.
type boardFix struct{ maker, marking, modelID, soc, kind string }

// suggestedBoard is the board the review page would add for a report's
// proposal, corrected by fix, with the catalogue to resolve its SoC. A
// report that proposed nothing gets one from fix alone.
func suggestedBoard(ctx context.Context, cfg *config.Config, st *reports.Store, id string, fix boardFix) (*boards.NewModel, error) {
	p, err := st.ProposalOf(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		p = &reports.Proposal{}
	}
	p.Maker, p.Board, p.SoC = or(fix.maker, p.Maker), or(fix.marking, p.Board), or(fix.soc, p.SoC)
	if p.Maker == "" || p.Board == "" {
		return nil, fmt.Errorf("%s proposes no new camera; link it with --model, or name one with --maker and --marking", id)
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
	sug.ModelID, sug.Kind = or(fix.modelID, sug.ModelID), fix.kind
	if cat, err := catalogue.Load(cfg.CatalogueDir); err == nil {
		st.SoC = socResolver(cat)
	}
	return &sug.NewModel, nil
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
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
