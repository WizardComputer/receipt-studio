package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"receiptstudio.local/agent/internal/agent"
	"receiptstudio.local/agent/internal/api"
	"receiptstudio.local/agent/internal/desktop"
	"receiptstudio.local/agent/internal/printer"
)

func Main() {
	if len(os.Args) > 1 && os.Args[1] == "--local-printer-worker" {
		printer.RunLocalPrinterWorker()
		return
	}
	if err := run(); err != nil {
		log.Print(err)
		desktop.ReportDesktopError(err.Error())
		os.Exit(1)
	}
}

func run() error {
	defaultDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	address := flag.String("listen", "127.0.0.1:17840", "Loopback HTTP address")
	dir := flag.String("data-dir", filepath.Join(defaultDir, "ReceiptStudio"), "Configuration and durable job storage")
	cert := flag.String("tls-cert", "", "Optional trusted TLS certificate")
	key := flag.String("tls-key", "", "Optional TLS private key")
	headless := flag.Bool("headless", !desktop.DesktopBuild, "Run without tray or desktop integration")
	setup := flag.Bool("setup-autostart", true, "Enable login startup on first desktop launch")
	flag.Parse()
	desktop.DesktopActive = desktop.DesktopBuild && !*headless
	if err := os.MkdirAll(*dir, 0700); err != nil {
		return err
	}
	unlock, err := lockDataDir(*dir)
	if err != nil {
		return fmt.Errorf("Agent je već pokrenut ili nije moguće zaključati podatke: %w", err)
	}
	defer unlock()
	host, _, err := net.SplitHostPort(*address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("Agent smije slušati samo na loopback IP adresi.")
	}
	if (*cert == "") != (*key == "") {
		return fmt.Errorf("TLS zahtijeva i certifikat i ključ.")
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return fmt.Errorf("Nije moguće otvoriti %s. Provjeri da drugi agent nije pokrenut: %w", *address, err)
	}
	defer listener.Close()
	agent, err := agent.NewAgent(*dir)
	if err != nil {
		return err
	}
	defer agent.Close()
	log.SetOutput(agent.LogWriter())
	defer log.SetOutput(os.Stderr)
	agent.RecordDiagnostic("info", "database", "SQLite baza je otvorena i konfiguracija je učitana.", "")
	agent.RecordDiagnostic("info", "agent", "Agent je pokrenut.", "")
	scheme := "http"
	if *cert != "" {
		scheme = "https"
	}
	agent.SetEndpoint(scheme+"://"+*address, *address)
	server := &http.Server{Addr: *address, Handler: api.New(agent, agent.Origin(), *address), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	diagnosticsDone := agent.StartDiagnosticMaintenance(ctx)
	go agent.Worker(ctx)
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("\nReceipt Studio Agent\n  API: %s\n  Podaci: %s\n  Zaustavi: Ctrl+C\n\n", agent.Origin(), *dir)
	result := make(chan error, 1)
	var shutdownOnce sync.Once
	var serveResult error
	finish := func() error {
		shutdownOnce.Do(func() {
			serveResult = <-result
			if serveResult != nil && !errors.Is(serveResult, http.ErrServerClosed) {
				agent.RecordDiagnostic("error", "api", "API server je neočekivano zaustavljen.", "")
			} else {
				agent.RecordDiagnostic("info", "agent", "Agent je uredno zaustavljen.", "")
			}
		})
		return serveResult
	}
	go func() {
		var serveErr error
		if *cert != "" {
			serveErr = server.ServeTLS(listener, *cert, *key)
		} else {
			serveErr = server.Serve(listener)
		}
		result <- serveErr
		stop()
	}()
	if desktop.DesktopActive {
		// Cocoa terminates the process after its exit callback, so finish all
		// durable writes inside that callback instead of relying on main returning.
		desktop.RunDesktop(ctx, func() {
			stop()
			<-shutdownDone
			<-agent.WorkerDone()
			<-diagnosticsDone
			finish()
			log.SetOutput(os.Stderr)
			_ = agent.Close()
		}, agent.Origin(), *dir, os.Args[1:], *setup, agent)
	} else {
		<-ctx.Done()
	}
	<-shutdownDone
	<-agent.WorkerDone()
	<-diagnosticsDone
	err = finish()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
