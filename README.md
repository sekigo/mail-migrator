# mail-migrator

Утилита для переноса писем между IMAP-серверами (папка за папкой, с сохранением
флагов и даты получения).
## Архитектура

```
cmd/mail-migrator/     — CLI: флаги, graceful shutdown по Ctrl+C/SIGTERM
internal/imapclient/   — обёртка над go-imap: Connect с TLS/STARTTLS
internal/migrator/      — вся бизнес-логика:
  types.go       — Config, FolderStats (без побочных эффектов)
  migrator.go    — маппинг папок, идемпотентность, worker pool
  migrator_test.go — table-driven тесты на чистые функции
```

Ключевые решения:

- **Идемпотентность**: перед копированием письма собирается набор
  `Message-Id`, уже присутствующих в целевой папке (`existingMessageIDs`).
  Повторный запуск после сбоя не создаёт дублей.
- **Конкурентность через errgroup + семафор**: каждая папка — отдельная
  задача; воркер держит собственную пару IMAP-соединений (клиенты go-imap
  не потокобезопасны, шарить одно соединение между горутинами нельзя).
- **Graceful shutdown**: `signal.NotifyContext` создаёт контекст, отменяемый
  по Ctrl+C. Внутри цикла обработки писем проверяется `ctx.Done()` —
  текущее письмо докопируется, новые не начинаются.
- **Разные разделители иерархии папок**: `MapFolderName` — чистая функция,
  переводит `INBOX.Work` (Dovecot, разделитель `.`) в `INBOX/Work`
  (некоторые сервера используют `/`). Покрыта тестами без поднятия сервера.

## Установка зависимостей

```bash
go mod tidy
```

## Запуск

```bash
go run ./cmd/mail-migrator \
  --src-addr imap.source.example.com:993 --src-user alice --src-pass '...' \
  --dst-addr imap.dest.example.com:993   --dst-user alice --dst-pass '...' \
  --workers 4 --dry-run
```

## Тесты

```bash
go test ./... -race -v
```


