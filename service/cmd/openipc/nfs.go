package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/nfsro"
)

// nfsRole is `openipc serve --role nfs`: TOOLS_ROOT, read-only over NFS, for
// a camera on stock firmware that can mount but cannot fetch
// (`mount -o nolock openipc.org:/ipctool /tmp/o`). No database: it serves the
// files the web role's tools push put there. /up on :3004 for the health
// check.
func nfsRole(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	if _, err := os.Stat(cfg.ToolsRoot); err != nil {
		return fmt.Errorf("TOOLS_ROOT: %w", err)
	}
	_, portStr, err := net.SplitHostPort(cfg.NFSAddr)
	if err != nil {
		return fmt.Errorf("NFS_ADDR: %w", err)
	}
	port, _ := strconv.Atoi(portStr)
	// Handles are keyed to a secret made at start: a restart makes old
	// handles stale, and a camera simply mounts again.
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	s := &nfsro.Server{Root: cfg.ToolsRoot, Secret: secret, NFSPort: uint32(port), Log: log}

	errc := make(chan error, 5)
	for _, addr := range []string{cfg.PortmapAddr, cfg.NFSAddr} {
		pc, err := net.ListenPacket("udp", addr)
		if err != nil {
			return err
		}
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		go func() { errc <- s.ServeUDP(ctx, pc) }()
		go func() { errc <- s.ServeTCP(ctx, l) }()
		log.Info("listening", "addr", addr)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /up", func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.ReadDir(cfg.ToolsRoot); err != nil {
			http.Error(w, "tools directory unreadable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	srv := &http.Server{Addr: ":3004", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		return nil
	case err := <-errc:
		return err
	}
}
