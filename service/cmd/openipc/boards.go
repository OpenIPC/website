package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/boards"
	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/config"
)

// boardsCommand is `openipc boards import-openhisiipcam`: once per
// environment, the OpenHisiIpCam project's board archive (firmware#659) into
// the board catalogue, its files under BOARDS_ROOT. A second run adds nothing.
func boardsCommand(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	if len(args) > 0 && args[0] == "import-snapshot" {
		return importSnapshot(ctx, cfg, log, args[1:])
	}
	if len(args) == 0 || args[0] != "import-openhisiipcam" {
		return fmt.Errorf("usage: openipc boards import-openhisiipcam [--from <docs/hardware directory>] | import-snapshot --source <id> <snapshot.tar>")
	}
	fs := flag.NewFlagSet("import-openhisiipcam", flag.ExitOnError)
	from := fs.String("from", "", "a checkout's docs/hardware, instead of downloading the pinned commit")
	_ = fs.Parse(args[1:])

	cat, err := catalogue.Load(cfg.CatalogueDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.BoardsRoot, 0o755); err != nil {
		return err
	}
	pool, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	im := &boards.Importer{Pool: pool, Log: log, Root: cfg.BoardsRoot,
		HTTP: &http.Client{Timeout: 10 * time.Minute}, TarballURL: boards.TarballURL(),
		Resolve: func(label string) string {
			if s := cat.SoC(label); s != nil {
				return s.URLName
			}
			return ""
		}}
	var n int
	if *from != "" {
		n, err = im.FromFS(ctx, os.DirFS(*from))
	} else {
		n, err = im.FromTarball(ctx)
	}
	if err != nil {
		return err
	}
	log.Info("boards: import done", "added", n, "ref", boards.OpenHisiIpCamRef)
	return nil
}

// importSnapshot is `openipc boards import-snapshot --source <id> <tar>`: a
// donor's catalogue as tools/board-donors prepared it. The tar must be the
// one pinned in boards.Snapshots; a model any source already has gains this
// source's texts, photos and links rather than a second card.
func importSnapshot(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("import-snapshot", flag.ExitOnError)
	source := fs.String("source", "", "the donor the snapshot is from (cctvsp, xiongmai)")
	unpinned := fs.Bool("allow-unpinned", false, "import a snapshot that is not pinned: local previews only, never an environment")
	_ = fs.Parse(args)
	if *source == "" || fs.NArg() != 1 {
		return fmt.Errorf("usage: openipc boards import-snapshot --source <id> <snapshot.tar>")
	}
	file := fs.Arg(0)
	if !*unpinned {
		if err := boards.VerifySnapshot(file, *source); err != nil {
			return err
		}
	} else {
		log.Warn("boards: importing an unpinned snapshot", "file", file)
	}
	cat, err := catalogue.Load(cfg.CatalogueDir)
	if err != nil {
		return err
	}
	pool, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	im := &boards.Importer{Pool: pool, Log: log, Root: cfg.BoardsRoot,
		Resolve: func(label string) string {
			if s := cat.SoC(label); s != nil {
				return s.URLName
			}
			return ""
		}}
	n, err := im.FromSnapshotTar(ctx, file)
	if err != nil {
		return err
	}
	log.Info("boards: snapshot imported", "source", *source, "new_models", n)
	// A confirmation or a contribution waiting for a model this snapshot
	// brought applies now.
	if err := applyConfirmations(ctx, pool, log); err != nil {
		return err
	}
	return applyContributions(ctx, pool, log, cfg.BoardsRoot)
}

// applyContributions publishes contributions.yml: what owners sent about
// their own boards, as units credited to them. An entry whose model the
// catalogue does not have yet waits, reported, for the import that brings it.
func applyContributions(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, root string) error {
	list, err := boards.Contributions()
	if err != nil {
		return err
	}
	im := &boards.Importer{Pool: pool, Log: log, Root: root}
	missing, err := im.ApplyContributions(ctx, list)
	if err != nil {
		return fmt.Errorf("boards: contributions.yml: %w", err)
	}
	for _, m := range missing {
		log.Warn("boards: contributions.yml names a model the catalogue does not have", "model", m)
	}
	log.Info("boards: contributions applied", "units", len(list)-len(missing))
	return nil
}

// applyConfirmations publishes contents.yml: what owners found inside the
// devices they bought. A line whose device the catalogue does not have yet
// waits, reported, for the snapshot that brings it.
func applyConfirmations(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	list, err := boards.Confirmations()
	if err != nil {
		return err
	}
	missing, err := boards.ApplyConfirmations(ctx, pool, list)
	if err != nil {
		return fmt.Errorf("boards: contents.yml: %w", err)
	}
	for _, m := range missing {
		log.Warn("boards: contents.yml names a device the catalogue does not have", "device", m)
	}
	log.Info("boards: confirmed contents applied", "lines", len(list)-len(missing))
	return nil
}
