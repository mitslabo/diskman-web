package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"diskman-web/config"
	"diskman-web/web"
)

// daemonize detaches the process from its controlling terminal.
// After calling, the process runs in the background as a daemon.
func daemonize() error {
	// Rebuild args for child without daemon flags to avoid recursion.
	childArgs := make([]string, 0, len(os.Args)-1)
	for i := 1; i < len(os.Args); i++ {
		a := os.Args[i]
		if a == "--daemon" || a == "-daemon" {
			continue
		}
		if strings.HasPrefix(a, "--daemon=") || strings.HasPrefix(a, "-daemon=") {
			continue
		}
		childArgs = append(childArgs, a)
	}

	devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open /dev/null failed: %w", err)
	}
	defer devNull.Close()

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path failed: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd failed: %w", err)
	}

	cmd := exec.Command(exePath, childArgs...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start detached child failed: %w", err)
	}

	// Parent exits immediately; child continues as daemon.
	os.Exit(0)
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
