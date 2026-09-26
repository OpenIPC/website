package main

import (
	"flag"
	"log/slog"

	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/firmware"
	"github.com/OpenIPC/website/service/internal/wizard"
)

// wizardExport writes /api/v1/wizard/<soc>.json for every SoC (#300): what the
// static wizard pages render their commands from. Hourly from cron, inside the
// firmware container, which has the catalogue and the release index.
func wizardExport(cfg *config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("wizard-export", flag.ExitOnError)
	out := fs.String("out", "/srv/wizard", "directory to write <soc>.json into")
	index := fs.String("index", cfg.ReleaseIndexPath, "the release index")
	_ = fs.Parse(args)
	cat, err := catalogue.Load(cfg.CatalogueDir)
	if err != nil {
		return err
	}
	idx, err := (&firmware.IndexFile{Path: *index}).Current()
	if err != nil {
		return err
	}
	files, combos, err := wizard.WriteAll(cat, idx, *out)
	if err != nil {
		return err
	}
	log.Info("wizard export", "files", files, "combinations", combos, "dir", *out)
	return nil
}
