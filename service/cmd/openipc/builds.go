package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/OpenIPC/website/service/internal/builds"
	"github.com/OpenIPC/website/service/internal/config"
)

// buildsCommand is `openipc builds import-history`: once per environment, the
// builds GitHub still holds, converted into the builds tables. After it, the
// CIs push every new build themselves (internal/builds/PUSH.md).
func buildsCommand(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	if len(args) == 0 || args[0] != "import-history" {
		return fmt.Errorf("usage: openipc builds import-history [--keep 90] [--kconfig-all] [--skip-builder]")
	}
	fs := flag.NewFlagSet("import-history", flag.ExitOnError)
	keep := fs.Int("keep", 90, "dated builds per source, as upstream's cleanup keeps")
	kconfigAll := fs.Bool("kconfig-all", false, "import every build's kconfig graphs, not only the newest")
	skipBuilder := fs.Bool("skip-builder", false, "firmware and u-boot only")
	_ = fs.Parse(args[1:])

	pool, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	h := &builds.History{Pool: pool, Log: log, Token: os.Getenv("GITHUB_TOKEN"),
		API: "https://api.github.com", HTTP: &http.Client{Timeout: 2 * time.Minute},
		Keep: *keep, KconfigAll: *kconfigAll,
		ManifestURL: "https://openipc.github.io/firmware/manifest.json"}
	n, err := h.Import(ctx, "OpenIPC/firmware", "firmware")
	if err != nil {
		return err
	}
	log.Info("history: firmware done", "imported", n)
	if err := h.ImportUBoot(ctx, "OpenIPC/firmware"); err != nil {
		return err
	}
	if !*skipBuilder {
		n, err = h.Import(ctx, "OpenIPC/builder", "builder")
		if err != nil {
			return err
		}
		log.Info("history: builder done", "imported", n)
	}
	return nil
}
