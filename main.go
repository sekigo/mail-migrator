package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourname/mail-migrator/internal/imapclient"
	"github.com/yourname/mail-migrator/internal/migrator"
)

func main() {
	var (
		srcAddr, srcUser, srcPass string
		dstAddr, dstUser, dstPass string
		workers                   int
		dryRun                    bool
		srcTLS, dstTLS            bool
	)

	flag.StringVar(&srcAddr, "src-addr", "", "source IMAP host:port")
	flag.StringVar(&srcUser, "src-user", "", "source username")
	flag.StringVar(&srcPass, "src-pass", "", "source password")
	flag.BoolVar(&srcTLS, "src-tls", true, "use implicit TLS for source (port 993)")

	flag.StringVar(&dstAddr, "dst-addr", "", "destination IMAP host:port")
	flag.StringVar(&dstUser, "dst-user", "", "destination username")
	flag.StringVar(&dstPass, "dst-pass", "", "destination password")
	flag.BoolVar(&dstTLS, "dst-tls", true, "use implicit TLS for destination (port 993)")

	flag.IntVar(&workers, "workers", 4, "number of folders migrated concurrently")
	flag.BoolVar(&dryRun, "dry-run", false, "only report what would be migrated")
	flag.Parse()

	if srcAddr == "" || dstAddr == "" {
		slog.Error("src-addr and dst-addr are required")
		os.Exit(1)
	}

	// Ctrl+C / SIGTERM: даём текущим воркерам шанс красиво остановиться
	// (см. проверку ctx.Done() внутри migrateFolder), а не убиваем процесс.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := migrator.Config{
		Source: imapclient.Config{Addr: srcAddr, Username: srcUser, Password: srcPass, UseTLS: srcTLS},
		Dest:   imapclient.Config{Addr: dstAddr, Username: dstUser, Password: dstPass, UseTLS: dstTLS},
		Workers: workers,
		DryRun:  dryRun,
	}

	results, err := migrator.Run(ctx, cfg)
	if err != nil {
		slog.Error("migration failed", "err", err)
	}

	var totalMigrated, totalSkipped, totalFailed int
	for _, r := range results {
		if r.LastError != nil {
			slog.Warn("folder finished with error", "folder", r.Folder, "err", r.LastError)
		}
		slog.Info("folder done",
			"folder", r.Folder,
			"total", r.Total,
			"migrated", r.Migrated,
			"skipped", r.Skipped,
			"failed", r.Failed,
		)
		totalMigrated += r.Migrated
		totalSkipped += r.Skipped
		totalFailed += r.Failed
	}

	slog.Info("migration summary",
		"folders", len(results),
		"migrated", totalMigrated,
		"skipped", totalSkipped,
		"failed", totalFailed,
	)

	if totalFailed > 0 {
		os.Exit(1)
	}
}
