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

// daemonize detaches the process from its controlling terminal.
// After calling, the process runs in the background as a daemon.
func daemonize() error {
	// Create a new session and become session leader
	// This disconnects from the controlling terminal
	sid, err := syscall.Setsid()
	if err != nil {
		return fmt.Errorf("setsid failed: %w", err)
	}
	if sid == -1 {
		return fmt.Errorf("setsid failed: sid is -1")
	}

	// Change working directory to root to avoid holding any directory
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir failed: %w", err)
	}

	// Set umask to 0
	syscall.Umask(0)

	// Redirect standard file descriptors to /dev/null
	null, err := os.Open("/dev/null")
	if err != nil {
		return fmt.Errorf("open /dev/null failed: %w", err)
	}
	defer null.Close()

	// Redirect stdin, stdout, stderr to /dev/null
	if err := syscall.Dup2(int(null.Fd()), 0); err != nil {
		return fmt.Errorf("dup2 stdin failed: %w", err)
	}
	if err := syscall.Dup2(int(null.Fd()), 1); err != nil {
		return fmt.Errorf("dup2 stdout failed: %w", err)
	}
	if err := syscall.Dup2(int(null.Fd()), 2); err != nil {
		return fmt.Errorf("dup2 stderr failed: %w", err)
	}

	return nil
}

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
	daemon := flag.Bool("daemon", false, "run as background daemon process (detach from terminal)")
	flag.Parse()

	// Daemonize only if explicitly requested
	if *daemon {
		if err := daemonize(); err != nil {
			fmt.Fprintf(os.Stderr, "daemonize failed: %v\n", err)
			os.Exit(1)
		}
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

	srv, err := web.NewServer(cfg, effectivePath, *enclosure)
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
