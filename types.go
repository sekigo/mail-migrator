package migrator

import "github.com/yourname/mail-migrator/internal/imapclient"

// Config — параметры одного запуска миграции.
type Config struct {
	Source imapclient.Config
	Dest   imapclient.Config

	// Workers — сколько папок обрабатывается параллельно.
	// IMAP-соединение не потокобезопасно для одного *client.Client,
	// поэтому на каждого воркера открывается своя пара соединений
	// source/dest, а не шарится один клиент между горутинами.
	Workers int

	// DryRun — только посчитать, что было бы перенесено, ничего не писать в Dest.
	DryRun bool
}

// FolderStats — результат обработки одной папки. Собирается воркером
// и отправляется в канал результатов для агрегации в главной горутине.
type FolderStats struct {
	Folder    string
	Total     int // писем найдено в source
	Migrated  int // реально скопировано
	Skipped   int // уже были в dest (идемпотентность по Message-ID)
	Failed    int
	LastError error
}

// migrationTask — единица работы для воркера: одна папка source
// плюс уже посчитанное имя папки в dest (с учётом разделителя иерархии).
type migrationTask struct {
	sourceFolder string
	destFolder   string
}
