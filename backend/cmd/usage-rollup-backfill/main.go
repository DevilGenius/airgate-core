package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DevilGenius/airgate-core/internal/config"
	"github.com/DevilGenius/airgate-core/internal/usageprojection"
	_ "github.com/lib/pq"
)

func main() {
	path := flag.String("config", "../.dev/config.yaml", "Core configuration; run as the core database owner")
	batch := flag.Int("batch-size", 2000, "Historical rows per short transaction (1-10000)")
	force := flag.Bool("rebuild", false, "Rebuild even when existing projections are verified")
	timeout := flag.Duration("timeout", 30*time.Minute, "Total maintenance deadline; interruption is resumable")
	verifyTimeout := flag.Duration("verify-timeout", 5*time.Minute, "Consistent-snapshot verification deadline (maximum 30m)")
	flag.Parse()
	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeout must be positive")
		os.Exit(1)
	}
	cfg, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load core configuration failed")
		os.Exit(1)
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database failed")
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	var lastReport time.Time
	err = usageprojection.Rebuild(ctx, db, usageprojection.RebuildOptions{
		BatchSize: *batch, Force: *force, VerifyTimeout: *verifyTimeout,
		Progress: func(progress usageprojection.Progress) {
			if time.Since(lastReport) < 2*time.Second && progress.State == "backfilling" {
				return
			}
			lastReport = time.Now()
			_ = json.NewEncoder(os.Stdout).Encode(progress)
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Usage rollup maintenance completed.")
}
