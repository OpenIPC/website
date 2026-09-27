package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/vendorfw"
)

// vendorFirmwareCommand is `openipc vendor-firmware import-history`: once per
// environment, what xmupdates and coupler have published so far. Their CI
// pushes keep it current from then on (vendorfw/PUSH.md).
func vendorFirmwareCommand(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	if len(args) == 0 || args[0] != "import-history" {
		return fmt.Errorf("usage: openipc vendor-firmware import-history")
	}
	pool, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	n, err := vendorfw.ImportHistory(ctx, pool, &http.Client{Timeout: 2 * time.Minute}, os.Getenv("GITHUB_TOKEN"))
	if err != nil {
		return err
	}
	log.Info("vendor-firmware: imported", "xmupdates", n["xmupdates"], "coupler", n["coupler"])
	return nil
}
