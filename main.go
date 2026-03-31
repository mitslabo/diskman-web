package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"diskman-web/config"
	"diskman-web/web"
)

func defaultConfigPath() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "diskman-web", "config.json")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "diskman-web", "config.json")
	}
	return "config.json"
}

func main() {
	configPath := flag.String("config", "", "path to config file (default: platform config dir)")
	enclosure := flag.String("enclosure", "", "active enclosure name (overrides config activeEnclosure)")
	addr := flag.String("addr", "", "listen address (e.g. :8080); overrides config")
	dryRun := flag.Bool("dry-run", false, "do not execute ddrescue/dd; simulate progress")
	debug := flag.Bool("debug", false, "enable debug mode (mock /dev/diskN paths, implies dry-run)")
	flag.Parse()

	if *debug {
		*dryRun = true
	}

	configSpecified := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configSpecified = true
		}
	})

	effectivePath := *configPath
	if effectivePath == "" {
		effectivePath = defaultConfigPath()
	}

	cfg, err := config.Load(effectivePath, configSpecified)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load error:", err)
		os.Exit(1)
	}

	if *addr != "" {
		cfg.Addr = *addr
	}

	srv, err := web.NewServer(cfg, effectivePath, *enclosure, *dryRun, *debug)
	if err != nil {
		fmt.Fprintln(os.Stderr, "server init error:", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:    cfg.Addr,
		Handler: srv.Handler(),
	}

	go func() {
		fmt.Printf("diskman-web starting on http://%s\n", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Println("\nshutting down...")
}
