package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/OpenIPC/website/service/internal/boards"
	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/config"
)

// boardsCommand is `openipc boards import-openhisiipcam`: once per
// environment, the OpenHisiIpCam project's board archive (firmware#659) into
// the board catalogue, its files under BOARDS_ROOT. A second run adds nothing.
func boardsCommand(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	if len(args) == 0 || args[0] != "import-openhisiipcam" {
		return fmt.Errorf("usage: openipc boards import-openhisiipcam [--from <docs/hardware directory>]")
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
