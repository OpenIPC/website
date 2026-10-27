package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/boards"
	"github.com/OpenIPC/website/service/internal/club"
	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/crashes"
	"github.com/OpenIPC/website/service/internal/reports"
	"github.com/OpenIPC/website/service/internal/snapshots"
	"github.com/OpenIPC/website/service/internal/wallstars"
)

// newClub builds the club from the settings. A way in that is not
// configured is simply not offered; the bot learns its name and sets its
// webhook in the background, and until it has, Telegram is not offered.
func newClub(bg context.Context, cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool, ownerReports *reports.API, wallStars *wallstars.Store) *club.API {
	httpc := &http.Client{Timeout: 20 * time.Second}
	api := &club.API{DB: pool, Log: log, Reports: ownerReports, Wall: wallStars,
		Cfg:        club.Config{SiteURL: cfg.ClubSiteURL, Mirrors: cfg.ClubMirrorOrigins, MaintainerOrg: cfg.ClubMaintainerOrg, Maintainers: cfg.ClubMaintainers},
		OnReviewed: func(ctx context.Context) { refreshReportUnits(ctx, cfg, log, pool) },
	}
	if cfg.TelegramBotToken != "" {
		api.Telegram = &club.Telegram{Token: cfg.TelegramBotToken, API: "https://api.telegram.org", HTTP: httpc, Log: log}
		go func() {
			for attempt := 0; ; attempt++ {
				err := api.Telegram.Start(bg, cfg.ClubSiteURL)
				if err == nil {
					log.Info("club: telegram bot ready", "bot", api.Telegram.Username())
					return
				}
				log.Warn("club: telegram bot not ready", "err", err)
				select {
				case <-bg.Done():
					return
				case <-time.After(time.Duration(min(attempt+1, 10)) * time.Minute):
				}
			}
		}()
	}
	if cfg.GitHubClientID != "" && cfg.GitHubClientSecret != "" {
		api.GitHub = &club.GitHub{ClientID: cfg.GitHubClientID, Secret: cfg.GitHubClientSecret,
			Web: "https://github.com", API: "https://api.github.com", HTTP: httpc}
	}
	if cfg.SMTPAddr != "" {
		api.Mail = &club.SMTP{Addr: cfg.SMTPAddr, User: cfg.SMTPUser, Password: cfg.SMTPPassword, From: cfg.MailFrom}
	}
	return api
}

// refreshReportUnits lists every published owner report's text and photos
// on the boards it was linked to, as contributed units: searchable and
// counted like any board's files. It runs at start and after each review;
// a failure is logged, and the next run catches up.
func refreshReportUnits(ctx context.Context, cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool) {
	texts, err := (&reports.Store{DB: pool}).PublishedTexts(ctx)
	if err != nil {
		log.Error("boards: published reports unreadable", "err", err)
		return
	}
	kinds := map[string]string{"boot_log": "boot_log", "uboot_env": "uboot_env", "note": "note"}
	var list []boards.Contribution
	for _, t := range texts {
		by := t.By
		if by == "" {
			by = "an owner"
		}
		// A report published on several boards is a unit on each, and each
		// unit's reference is its own (board_units.source_ref is unique).
		// The sender's note -- where it was bought, what it is sold as -- is
		// the unit's, as published (redacted).
		// The link's certainty goes with it, so the board's files say how
		// sure it is that the report is this board's.
		c := boards.Contribution{Unit: t.Model + "-" + t.Report, Model: t.Model, By: by, Note: t.Note,
			Evidence:      []string{cfg.ClubSiteURL + boards.ReceiptMark + t.Report + "&board=" + t.Model},
			LinkCertainty: t.Certainty, LinkEvidence: t.Evidence}
		for _, f := range t.Files {
			kind := kinds[f.Kind]
			if f.Kind == "photo" {
				kind = boards.ReportPhotoKind(f.Name)
			}
			// A photo a reviewer blurred is named after its copy: the board's
			// files are cached for a month (BoardName).
			c.Files = append(c.Files, boards.ContributedFile{Kind: kind, File: f.BoardName(), Source: f.Path})
		}
		list = append(list, c)
	}
	im := &boards.Importer{Pool: pool, Log: log, Root: cfg.BoardsRoot}
	missing, err := im.ApplyReportUnits(ctx, os.DirFS(cfg.ReportsRoot), list)
	if err != nil {
		log.Error("boards: published reports not listed", "err", err)
		return
	}
	for _, m := range missing {
		log.Warn("boards: a published report names a board the catalogue does not have", "model", m)
	}
	// A board a review added goes once its report is rejected and nothing
	// else is on it (#452) -- after the units, so the report's unit and its
	// files have gone first.
	gone, err := boards.RemoveAbandoned(ctx, pool)
	if err != nil {
		log.Error("boards: abandoned boards not removed", "err", err)
	}
	for _, m := range gone {
		log.Info("boards: a board its report's rejection left empty removed", "model", m)
	}
}

const clubUsage = `usage: openipc club
  settle-wall                          pay what linked cameras earned on the Open Wall, and tell their owners (also run by purge)
  wall-revoke <camera> --reason text   a camera found faked, by its public name or MAC: it earns nothing more, and its stars are taken back
  wall-unlink <camera>                 unlink a camera someone else linked first, so its owner can link it; what it earned stays
  merge <from> <into> --reason text [--dry-run]
                                       make two accounts of one person one, once they have shown both are theirs:
                                       everything of <from> becomes <into>'s, stars included, and <from> is deleted;
                                       --dry-run says what would move and changes nothing (JSON)`

// clubCommand is `openipc club ...`, run in the web container.
func clubCommand(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	if len(args) == 0 {
		return errors.New(clubUsage)
	}
	pool, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	switch args[0] {
	case "settle-wall":
		return settleWall(ctx, cfg, log, pool)
	case "wall-revoke":
		fs := flag.NewFlagSet("club wall-revoke", flag.ExitOnError)
		reason := fs.String("reason", "", "why, for the record")
		pos, err := parseInterleaved(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 || *reason == "" {
			return errors.New(clubUsage)
		}
		st := &wallstars.Store{DB: pool, Token: (&snapshots.Store{TokenKey: cfg.CameraTokenKey}).CameraToken}
		taken, err := st.Revoke(ctx, pos[0], *reason)
		if err != nil {
			return err
		}
		log.Info("wallstars: camera revoked", "camera", pos[0], "stars_taken_back", taken)
		return nil
	case "wall-unlink":
		if len(args) != 2 {
			return errors.New(clubUsage)
		}
		st := &wallstars.Store{DB: pool, Token: (&snapshots.Store{TokenKey: cfg.CameraTokenKey}).CameraToken}
		done, err := st.ForceUnlink(ctx, args[1])
		if err != nil {
			return err
		}
		log.Info("wallstars: camera unlinked", "camera", args[1], "was_linked", done)
		return nil
	case "merge":
		fs := flag.NewFlagSet("club merge", flag.ExitOnError)
		reason := fs.String("reason", "", "why, for the record: how the person showed both accounts are theirs")
		dry := fs.Bool("dry-run", false, "say what would move, change nothing")
		pos, err := parseInterleaved(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 2 || *reason == "" {
			return errors.New(clubUsage)
		}
		api := askClub(cfg, log, pool)
		api.Cfg.Maintainers = cfg.ClubMaintainers
		m, err := api.Merge(ctx, pos[0], pos[1], *reason, *dry)
		if err != nil {
			return err
		}
		if !m.DryRun {
			log.Info("club: members merged", "from", m.From, "into", m.Into, "stars", m.Total)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(m)
	}
	return errors.New(clubUsage)
}

// askClub is the club as `openipc reports ask` needs it: the bot and the
// mail relay to tell the sender, when they are configured. The bot is not
// started (no webhook is set); it only sends.
func askClub(cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool) *club.API {
	api := &club.API{DB: pool, Log: log, Reports: &reports.API{DB: pool}, Cfg: club.Config{SiteURL: cfg.ClubSiteURL}}
	if cfg.TelegramBotToken != "" {
		api.Telegram = &club.Telegram{Token: cfg.TelegramBotToken, API: "https://api.telegram.org",
			HTTP: &http.Client{Timeout: 20 * time.Second}, Log: log}
	}
	if cfg.SMTPAddr != "" {
		api.Mail = &club.SMTP{Addr: cfg.SMTPAddr, User: cfg.SMTPUser, Password: cfg.SMTPPassword, From: cfg.MailFrom}
	}
	return api
}

// settleWall is the nightly settlement of the Open Wall's stars. The bot
// writes to the owners when it is configured; it is not started here (no
// webhook is set), it only sends.
func settleWall(ctx context.Context, cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool) error {
	st := &wallstars.Store{DB: pool, Token: (&snapshots.Store{TokenKey: cfg.CameraTokenKey}).CameraToken}
	api := &club.API{DB: pool, Log: log, Reports: &reports.API{DB: pool}, Wall: st,
		Crashes: &crashes.API{DB: pool, Log: log}, Cfg: club.Config{SiteURL: cfg.ClubSiteURL}}
	if cfg.TelegramBotToken != "" {
		api.Telegram = &club.Telegram{Token: cfg.TelegramBotToken, API: "https://api.telegram.org",
			HTTP: &http.Client{Timeout: 20 * time.Second}, Log: log}
	}
	res, err := api.SettleWall(ctx)
	if err != nil {
		return err
	}
	log.Info("wallstars: settled", "cameras", res.Cameras, "stars", res.Awarded, "notices", len(res.Notices))
	return nil
}
