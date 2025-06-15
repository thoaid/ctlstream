package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/thoaid/ctlstream/internal/hub"
	"github.com/thoaid/ctlstream/internal/monitor"
)

var noCert = flag.Bool("nocert", false, "omit certificate PEM from output")

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		cancel()
	}()

	h := hub.New()
	go h.Run()

	http.HandleFunc("/ws", h.HandleWS)

	logs, err := monitor.FetchLogs(ctx)
	if err != nil {
		log.Fatalf("fetch logs: %v", err)
	}

	log.Printf("monitoring %d CT logs", len(logs))
	for _, lg := range logs {
		go monitor.MonitorLog(ctx, h, lg, *noCert)
	}

	srv := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	log.Printf("listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
