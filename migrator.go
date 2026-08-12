package migrator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"golang.org/x/sync/errgroup"

	"github.com/yourname/mail-migrator/internal/imapclient"
)

// MapFolderName переводит путь папки из разделителя source-сервера
// в разделитель dest-сервера. Это чистая функция без побочных эффектов —
// специально вынесена отдельно, чтобы покрыть table-driven тестами
// без поднятия реального IMAP-сервера.
//
// Пример: "INBOX.Work.2024" при srcDelim='.' и dstDelim='/'
// превращается в "INBOX/Work/2024".
func MapFolderName(name string, srcDelim, dstDelim byte) string {
	if srcDelim == dstDelim || srcDelim == 0 {
		return name
	}
	out := make([]byte, len(name))
	for i := 0; i < len(name); i++ {
		if name[i] == srcDelim {
			out[i] = dstDelim
		} else {
			out[i] = name[i]
		}
	}
	return string(out)
}

// listFolders возвращает список папок и разделитель иерархии сервера.
func listFolders(c *client.Client) ([]string, byte, error) {
	mailboxes := make(chan *imap.MailboxInfo, 16)
	done := make(chan error, 1)
	go func() { done <- c.List("", "*", mailboxes) }()

	var names []string
	var delim byte
	for m := range mailboxes {
		names = append(names, m.Name)
		if len(m.Delimiter) > 0 {
			delim = m.Delimiter[0]
		}
	}
	if err := <-done; err != nil {
		return nil, 0, fmt.Errorf("list folders: %w", err)
	}
	return names, delim, nil
}

// existingMessageIDs собирает Message-ID писем, уже лежащих в папке dest.
// Используется для идемпотентности: перезапуск после сбоя не должен
// создавать дубликаты.
func existingMessageIDs(c *client.Client, folder string) (map[string]struct{}, error) {
	if _, err := c.Select(folder, false); err != nil {
		// Папка ещё не создана на dest — считаем, что там пусто.
		return map[string]struct{}{}, nil
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddRange(1, 0) // 0 в AddRange означает "*", то есть все письма

	messages := make(chan *imap.Message, 32)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqSet, []imap.FetchItem{imap.FetchEnvelope}, messages)
	}()

	ids := make(map[string]struct{})
	for msg := range messages {
		if msg.Envelope != nil && msg.Envelope.MessageId != "" {
			ids[msg.Envelope.MessageId] = struct{}{}
		}
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("fetch envelopes in %s: %w", folder, err)
	}
	return ids, nil
}

// migrateFolder переносит одну папку: читает письма из source,
// пропускает те, что уже есть в dest (по Message-ID), остальные
// копирует с сохранением флагов и даты получения.
func migrateFolder(ctx context.Context, src, dst *client.Client, task migrationTask, dryRun bool) FolderStats {
	stats := FolderStats{Folder: task.sourceFolder}

	if _, err := src.Select(task.sourceFolder, true); err != nil {
		stats.LastError = fmt.Errorf("select source %s: %w", task.sourceFolder, err)
		return stats
	}

	alreadyInDest, err := existingMessageIDs(dst, task.destFolder)
	if err != nil {
		stats.LastError = err
		return stats
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddRange(1, 0)

	items := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchFlags,
		imap.FetchInternalDate,
		imap.FetchRFC822, // полное тело письма
	}

	messages := make(chan *imap.Message, 16)
	fetchDone := make(chan error, 1)
	go func() { fetchDone <- src.Fetch(seqSet, items, messages) }()

	for msg := range messages {
		select {
		case <-ctx.Done():
			// Graceful shutdown: не бросаем письмо на середине, просто
			// перестаём брать новые из канала и выходим с тем, что успели.
			stats.LastError = ctx.Err()
			return stats
		default:
		}

		stats.Total++

		if msg.Envelope != nil {
			if _, seen := alreadyInDest[msg.Envelope.MessageId]; seen {
				stats.Skipped++
				continue
			}
		}

		if dryRun {
			stats.Migrated++
			continue
		}

		body := msg.GetBody(&imap.BodySectionName{})
		if body == nil {
			stats.Failed++
			slog.Warn("empty body, skipping", "folder", task.sourceFolder, "uid", msg.Uid)
			continue
		}

		if err := dst.Append(task.destFolder, msg.Flags, msg.InternalDate, body); err != nil {
			stats.Failed++
			slog.Error("append failed", "folder", task.destFolder, "err", err)
			continue
		}
		stats.Migrated++
	}

	if err := <-fetchDone; err != nil {
		stats.LastError = fmt.Errorf("fetch %s: %w", task.sourceFolder, err)
	}
	return stats
}

// Run — точка входа: подключается к source/dest, строит список задач
// (папка source -> папка dest с учётом разных разделителей иерархии)
// и раздаёт их пулу воркеров через errgroup.
//
// Каждый воркер держит СВОИ соединения (клиенты go-imap не потокобезопасны),
// поэтому Run открывает Workers*2 соединений, а не переиспользует одно.
func Run(ctx context.Context, cfg Config) ([]FolderStats, error) {
	srcList, srcDelim, err := discoverFolders(cfg.Source)
	if err != nil {
		return nil, fmt.Errorf("discover source folders: %w", err)
	}

	_, dstDelim, err := discoverFoldersDelimOnly(cfg.Dest)
	if err != nil {
		return nil, fmt.Errorf("discover dest delimiter: %w", err)
	}

	tasks := make([]migrationTask, 0, len(srcList))
	for _, name := range srcList {
		tasks = append(tasks, migrationTask{
			sourceFolder: name,
			destFolder:   MapFolderName(name, srcDelim, dstDelim),
		})
	}

	results := make([]FolderStats, len(tasks))
	g, gctx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, cfg.Workers)

	for i, task := range tasks {
		i, task := i, task
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()

			src, err := imapclient.Connect(cfg.Source)
			if err != nil {
				results[i] = FolderStats{Folder: task.sourceFolder, LastError: err}
				return nil // не роняем весь errgroup из-за одной папки
			}
			defer src.Logout()

			dst, err := imapclient.Connect(cfg.Dest)
			if err != nil {
				results[i] = FolderStats{Folder: task.sourceFolder, LastError: err}
				return nil
			}
			defer dst.Logout()

			if !cfg.DryRun {
				dst.Create(task.destFolder) // игнорируем ошибку "уже существует"
			}

			results[i] = migrateFolder(gctx, src, dst, task, cfg.DryRun)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return results, err
	}
	return results, nil
}

func discoverFolders(cfg imapclient.Config) ([]string, byte, error) {
	c, err := imapclient.Connect(cfg)
	if err != nil {
		return nil, 0, err
	}
	defer c.Logout()
	return listFolders(c)
}

func discoverFoldersDelimOnly(cfg imapclient.Config) ([]string, byte, error) {
	return discoverFolders(cfg)
}
