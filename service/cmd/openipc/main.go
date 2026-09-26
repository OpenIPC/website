// Command openipc is the service behind openipc.org's dynamic addresses. One
// binary, several subcommands:
//
//	openipc serve --role web       uploads, variants, the wall, build pushes  (:3002)
//	openipc serve --role firmware  full images, their stats, the wizard, availability  (:3003)
//	openipc migrate                bring PostgreSQL to this binary's schema
//	openipc purge [--snapshots] [--firmware] [--builds]   nightly retention
//	openipc probe                  nightly health numbers, non-zero on trouble
//	openipc builds import-history  once: the builds GitHub still holds, into PostgreSQL
//	openipc routes --json          what this binary answers, for the nginx seam test
//
// Configuration is the environment; see internal/config.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/builds"
	"github.com/OpenIPC/website/service/internal/catalogue"
	"github.com/OpenIPC/website/service/internal/config"
	"github.com/OpenIPC/website/service/internal/db"
	"github.com/OpenIPC/website/service/internal/downloads"
	"github.com/OpenIPC/website/service/internal/firmware"
	"github.com/OpenIPC/website/service/internal/httpx"
	"github.com/OpenIPC/website/service/internal/purge"
	"github.com/OpenIPC/website/service/internal/snapshots"
	"github.com/OpenIPC/website/service/internal/variants"
	"github.com/OpenIPC/website/service/internal/wall"
	"github.com/OpenIPC/website/service/internal/wallsocket"
	"github.com/OpenIPC/website/service/internal/wizard"
)

// version is stamped at build time (-ldflags "-X main.version=<sha>").
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	log := logger(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	args := os.Args[2:]
	switch os.Args[1] {
	case "serve":
		err = serve(ctx, cfg, log, args)
	case "migrate":
		err = migrate(ctx, cfg, log)
	case "purge":
		err = runPurge(ctx, cfg, log, args)
	case "probe":
		err = probe(ctx, cfg)
	case "builds":
		err = buildsCommand(ctx, cfg, log, args)
	case "routes":
		err = printRoutes()
	case "version":
		fmt.Println(version)
	default:
		usage()
	}
	if err != nil {
		fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: openipc serve --role web|firmware | migrate | purge [--snapshots] [--firmware] [--builds] | probe | builds import-history | routes --json | version")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "openipc:", err)
	os.Exit(1)
}

func logger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

func open(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	if err := cfg.RequireDatabase(); err != nil {
		return nil, err
	}
	return db.Open(ctx, cfg.DatabaseURL)
}

func migrate(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	pool, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	applied, err := db.Migrate(ctx, pool)
	log.Info("migrate", "applied", applied, "schema", db.Latest())
	return err
}

// Route is one address this binary answers, as the seam test reads it.
type Route struct {
	Role   string `json:"role"`
	Method string `json:"method"`
	Path   string `json:"path"`
}

// routes is the single list both muxes are built from, so what `routes
// --json` prints is what is served.
var routes = []Route{
	{"web", "POST", "/snapshots"},
	{"web", "POST", "/snapshots/{$}"},
	{"web", "POST", "/{locale}/snapshots"},
	{"web", "POST", "/{locale}/snapshots/{$}"},
	{"web", "GET", "/api/v1/wall/mosaic.json"},
	{"web", "GET", "/api/v1/wall/page/{page}"},
	{"web", "GET", "/api/v1/wall/snapshot/{file}"},
	{"web", "GET", "/api/v1/wall/snapshot/{id}/{file}"},
	{"web", "GET", "/api/v1/wall/camera/{file}"},
	{"firmware", "GET", "/api/v1/hardware/availability.json"},
	{"firmware", "GET", "/api/v1/wizard/{file}"},
	{"web", "POST", "/api/v1/builds"},
	{"web", "GET", "/api/v1/explorer/{source}/builds"},
	{"web", "GET", "/api/v1/explorer/{source}/builds/{build}/platforms/{platform}"},
	{"web", "GET", "/api/v1/explorer/{source}/platforms/{platform}/trends"},
	{"web", "GET", "/api/v1/explorer/{source}/platforms/{platform}/kconfig"},
	{"web", "GET", "/api/v1/wall/socket"},
	{"firmware", "GET", "/cameras/vendors/{vendor}/socs/{soc}/download_full_image"},
	{"firmware", "GET", "/{locale}/cameras/vendors/{vendor}/socs/{soc}/download_full_image"},
}

func printRoutes() error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(routes)
}

// prefixed accepts only the locales the site serves in a path.
func prefixed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l := r.PathValue("locale"); l != "" && l != "ru" && l != "zh" {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serve(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	role := fs.String("role", "", "web or firmware")
	listen := fs.String("listen", cfg.Listen, "address to listen on")
	_ = fs.Parse(args)
	if *listen == "" {
		*listen = map[string]string{"web": ":3002", "firmware": ":3003"}[*role]
	}
	log = log.With("role", *role, "version", version)

	pool, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.CheckCurrent(ctx, pool); err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /up", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(c); err != nil {
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "ok")
	})

	var background []func()
	switch *role {
	case "web":
		stopBg, err := web(ctx, cfg, log, pool, mux)
		if err != nil {
			return err
		}
		background = append(background, stopBg)
	case "firmware":
		stopBg, err := firmwareRole(ctx, cfg, log, pool, mux)
		if err != nil {
			return err
		}
		background = append(background, stopBg)
	default:
		return fmt.Errorf("--role must be web or firmware")
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           httpx.Log(log, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       90 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", *listen)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdown)
	for _, f := range background {
		f()
	}
	log.Info("stopped")
	return err
}

func web(ctx context.Context, cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool, mux *http.ServeMux) (func(), error) {
	if err := cfg.RequireWeb(); err != nil {
		return nil, err
	}
	// Exactly one web process per database: the variant queue, and the frame
	// budget #297 will add, assume it.
	lock, err := db.ClaimWebRole(ctx, pool)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.WallRoot, 0o755); err != nil {
		lock.Release()
		return nil, err
	}

	store := &snapshots.Store{DB: pool, TokenKey: cfg.CameraTokenKey}
	wallFS := variants.Wall{Root: cfg.WallRoot}
	proc := &variants.Processor{Wall: wallFS, Store: store, Log: log, Vips: cfg.VipsBin,
		VipsHeader: cfg.VipsHeaderBin, Workers: cfg.VariantWorkers}
	bg, cancel := context.WithCancel(context.WithoutCancel(ctx))
	if err := proc.Start(bg); err != nil {
		cancel()
		lock.Release()
		return nil, err
	}
	// The queue is the table: whatever a previous process left pending is
	// picked up now, and again every ten minutes in case an enqueue was
	// dropped.
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			if n, err := proc.Recover(bg); err != nil {
				log.Error("variants: sweep failed", "err", err)
			} else if n > 0 {
				log.Info("variants: sweep", "enqueued", n)
			}
			select {
			case <-bg.Done():
				return
			case <-t.C:
			}
		}
	}()

	upload := prefixed(&snapshots.UploadHandler{
		Store: store, Wall: wallFS, Enqueue: proc.Enqueue,
		Blacklist: cfg.MACBlacklist, Whitelist: cfg.IPWhitelist, Log: log, Shadow: cfg.Shadow,
	})
	granter := &wall.Granter{Key: cfg.WallGrantKey}
	// Every handler the web role has, keyed as the routes table names it. The
	// table decides what is served: a route with no handler, or a handler
	// the table does not list, stops the process from starting.
	handlers := map[string]http.Handler{
		// The one place builds enter: CI pushes each build once (builds/PUSH.md).
		"POST /api/v1/builds": &builds.Handler{
			Verifier: &builds.LazyVerifier{Issuer: builds.GitHubIssuer}, DB: pool, Log: log},
		"GET /api/v1/wall/socket": &wallsocket.Server{WallRoot: cfg.WallRoot, Grants: granter, Log: log,
			GrantsDisabled: cfg.GrantsDisabled, Budget: &wallsocket.Budget{Limit: 1000}},
	}
	for _, r := range routes {
		if r.Role == "web" && r.Method == "POST" && strings.Contains(r.Path, "/snapshots") {
			handlers[r.Method+" "+r.Path] = upload
		}
	}
	for k, h := range (&builds.Explorer{DB: pool, Log: log}).Handlers() {
		handlers[k] = h
	}
	for k, h := range (&wall.API{Store: store, Granter: granter, Log: log}).Handlers() {
		handlers[k] = h
	}
	for _, r := range routes {
		if r.Role != "web" {
			continue
		}
		k := r.Method + " " + r.Path
		h, ok := handlers[k]
		if !ok {
			cancel()
			lock.Release()
			return nil, fmt.Errorf("web route %s has no handler", k)
		}
		mux.Handle(k, h)
		delete(handlers, k)
	}
	for k := range handlers {
		cancel()
		lock.Release()
		return nil, fmt.Errorf("web handler %s is not in the routes table", k)
	}

	// The address this process believes a request came from, for the
	// remote_ip canary. Answered only to a peer on a trusted network, which is
	// nginx and the host -- never to the internet.
	mux.HandleFunc("GET /_whoami", func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); ip == nil || !(ip.IsLoopback() || ip.IsPrivate()) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, httpx.ClientIP(r))
	})

	return func() {
		cancel()
		proc.Wait()
		lock.Release()
	}, nil
}

func firmwareRole(ctx context.Context, cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool, mux *http.ServeMux) (func(), error) {
	cat, err := catalogue.Load(cfg.CatalogueDir)
	if err != nil {
		return nil, err
	}
	for _, dir := range []string{cfg.FirmwareCacheRoot, cfg.ReleaseCacheRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	releases := &firmware.Releases{Root: cfg.ReleaseCacheRoot, Base: cfg.DownloadBase, HTTP: firmware.NewHTTPClient()}
	images := &firmware.Images{Root: cfg.FirmwareCacheRoot, Releases: releases, MaxBytes: cfg.FirmwareCacheMax, Log: log}
	// The index is the builds tables, reloaded when a build is stored
	// (LISTEN builds). When it moves, the old version goes: images once nginx
	// has had the grace to finish with them, tarballs once no build is
	// reading them. The nightly purge catches anything still in its grace.
	index := &builds.Source{Pool: pool, Log: log, Changed: func(idx *firmware.Index) {
		n, freed := images.Keep(idx)
		var m int
		var freedTar int64
		if !images.Busy() {
			m, freedTar, _ = releases.Keep(idx)
		}
		if n+m > 0 {
			log.Info("firmware: evicted superseded versions", "images", n, "tarballs", m,
				"freed_mb", (freed+freedTar)>>20)
		}
	}}
	h := prefixed(&firmware.Handler{
		Catalogue: cat, Index: index, Images: images,
		Limiter:     &firmware.Limiter{Limit: cfg.BuildsPerMinute, Window: time.Minute},
		Downloads:   &downloads.Store{DB: pool},
		AccelPrefix: cfg.FirmwareAccelPrefix, Log: log,
	})
	// Every firmware route from the one table, so `openipc routes --json` and
	// what this mux serves cannot describe different sets.
	availability := &firmware.AvailabilityHandler{Catalogue: cat, Index: index}
	for _, r := range routes {
		if r.Role != "firmware" {
			continue
		}
		switch {
		case strings.HasSuffix(r.Path, "/download_full_image"):
			mux.Handle(r.Method+" "+r.Path, h)
		case r.Path == "/api/v1/hardware/availability.json":
			mux.Handle(r.Method+" "+r.Path, availability)
		case r.Path == "/api/v1/wizard/{file}":
			mux.Handle(r.Method+" "+r.Path, &wizard.Handler{Catalogue: cat, Index: index, Log: log})
		default:
			return nil, fmt.Errorf("firmware route %s %s has no handler", r.Method, r.Path)
		}
	}

	bg, cancel := context.WithCancel(context.WithoutCancel(ctx))
	go index.Run(bg)
	return cancel, nil
}

func runPurge(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("purge", flag.ExitOnError)
	doSnapshots := fs.Bool("snapshots", false, "retire snapshots past two days, with their images")
	doFirmware := fs.Bool("firmware", false, "evict firmware images and tarballs the index no longer describes")
	doBuilds := fs.Bool("builds", false, "keep the newest 90 builds per source, as upstream's release cleanup does")
	_ = fs.Parse(args)
	if !*doSnapshots && !*doFirmware && !*doBuilds {
		*doSnapshots, *doFirmware, *doBuilds = true, true, true
	}
	if *doBuilds {
		pool, err := open(ctx, cfg)
		if err != nil {
			return err
		}
		n, err := builds.Trim(ctx, pool, 90)
		pool.Close()
		if err != nil {
			return err
		}
		log.Info("purge: builds", "removed", n)
	}
	if *doFirmware {
		pool, err := open(ctx, cfg)
		if err != nil {
			return err
		}
		idx, err := builds.LoadIndex(ctx, pool)
		pool.Close()
		if err != nil {
			return err
		}
		releases := &firmware.Releases{Root: cfg.ReleaseCacheRoot}
		images := &firmware.Images{Root: cfg.FirmwareCacheRoot, Releases: releases}
		n, freed := images.Keep(idx)
		m, freedTar, err := releases.Keep(idx)
		if err != nil {
			return err
		}
		log.Info("purge: firmware", "images", n, "tarballs", m, "freed_mb", (freed+freedTar)>>20)
	}
	if *doSnapshots {
		pool, err := open(ctx, cfg)
		if err != nil {
			return err
		}
		defer pool.Close()
		lock, err := db.TryPurgeLock(ctx, pool)
		if err != nil {
			log.Info("purge: skipped", "reason", err.Error())
			return nil
		}
		defer lock.Release()
		p := &purge.Snapshots{DB: pool, WallRoot: cfg.WallRoot, MaxAge: cfg.SnapshotMaxAge, Log: log}
		rows, orphans, err := p.Run(ctx)
		log.Info("purge: snapshots", "rows", rows, "orphan_dirs", orphans)
		if err != nil {
			return err
		}
	}
	return nil
}

// probe prints the numbers that catch failures nothing else sees, and exits
// non-zero when one is wrong:
//   - an all-digit public_id (the CHECK should make this impossible) is a tile
//     answered 410 for two days;
//   - uploads with no HEIF among them for a day, when HEIF cameras exist, is a
//     content-type regression that silences those cameras one by one;
//   - frames whose variants have been pending for an hour are a stuck queue.
func probe(ctx context.Context, cfg *config.Config) error {
	pool, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	var out struct {
		Uploads24h    int `json:"uploads_24h"`
		Cameras24h    int `json:"cameras_24h"`
		HEIF24h       int `json:"heif_24h"`
		HEIFRetained  int `json:"heif_retained"`
		DigitIDs      int `json:"all_digit_public_ids"`
		StuckVariants int `json:"variants_pending_over_1h"`
		Downloads24h  int `json:"downloads_24h"`
	}
	err = pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE created_at > now() - interval '1 day'),
		count(DISTINCT mac_key) FILTER (WHERE created_at > now() - interval '1 day'),
		count(*) FILTER (WHERE created_at > now() - interval '1 day' AND content_type LIKE 'image/hei%'),
		count(*) FILTER (WHERE content_type LIKE 'image/hei%'),
		count(*) FILTER (WHERE public_id ~ '^[0-9]+$'),
		count(*) FILTER (WHERE variants_generated_at IS NULL AND created_at < now() - interval '1 hour')
		FROM snapshots`).Scan(&out.Uploads24h, &out.Cameras24h, &out.HEIF24h, &out.HEIFRetained, &out.DigitIDs, &out.StuckVariants)
	if err != nil {
		return err
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM downloads WHERE created_at > now() - interval '1 day'`).
		Scan(&out.Downloads24h); err != nil {
		return err
	}
	raw, _ := json.Marshal(out)
	fmt.Println(string(raw))
	var trouble []string
	if out.DigitIDs > 0 {
		trouble = append(trouble, "all-digit public_id present")
	}
	if out.StuckVariants > 0 {
		trouble = append(trouble, "variants pending for over an hour")
	}
	if out.HEIF24h == 0 && out.HEIFRetained > 0 && out.Uploads24h > 0 {
		trouble = append(trouble, "no HEIF uploads in a day although HEIF cameras exist")
	}
	if len(trouble) > 0 {
		return errors.New(strings.Join(trouble, "; "))
	}
	return nil
}
