package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/boards"
	"github.com/OpenIPC/website/service/internal/club"
	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/reports"
)

// newClub builds the club from the settings. A way in that is not
// configured is simply not offered; the bot learns its name and sets its
// webhook in the background, and until it has, Telegram is not offered.
func newClub(bg context.Context, cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool, ownerReports *reports.API) *club.API {
	httpc := &http.Client{Timeout: 20 * time.Second}
	api := &club.API{DB: pool, Log: log, Reports: ownerReports,
		Cfg:        club.Config{SiteURL: cfg.ClubSiteURL, MaintainerOrg: cfg.ClubMaintainerOrg, Maintainers: cfg.ClubMaintainers},
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
	kinds := map[string]string{"photo": "photo_other", "boot_log": "boot_log", "uboot_env": "uboot_env", "note": "note"}
	var list []boards.Contribution
	for _, t := range texts {
		by := t.By
		if by == "" {
			by = "an owner"
		}
		// A report published on several boards is a unit on each, and each
		// unit's reference is its own (board_units.source_ref is unique).
		c := boards.Contribution{Unit: t.Model + "-" + t.Report, Model: t.Model, By: by,
			Evidence: []string{cfg.ClubSiteURL + boards.ReceiptMark + t.Report + "&board=" + t.Model}}
		for _, f := range t.Files {
			c.Files = append(c.Files, boards.ContributedFile{Kind: kinds[f.Kind], File: fmt.Sprintf("%d-%s", f.Position, f.Name), Source: f.Path})
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
}
