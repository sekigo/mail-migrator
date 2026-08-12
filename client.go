// Package imapclient — тонкая обёртка над github.com/emersion/go-imap/client.
// Отделена от бизнес-логики миграции, чтобы:
//  1. её можно было мокать в тестах миграции без реального сервера;
//  2. не тащить детали протокола (TLS, AUTH) в migrator.
package imapclient

import (
	"crypto/tls"
	"fmt"

	"github.com/emersion/go-imap/client"
)

// Config описывает параметры подключения к одному IMAP-серверу.
type Config struct {
	Addr     string // host:port, например "imap.gmail.com:993"
	Username string
	Password string
	UseTLS   bool // true для порта 993 (implicit TLS), false для STARTTLS/25
}

// Connect устанавливает соединение и логинится.
// Возвращает уже аутентифицированный клиент, готовый к SELECT/FETCH.
func Connect(cfg Config) (*client.Client, error) {
	var c *client.Client
	var err error

	if cfg.UseTLS {
		c, err = client.DialTLS(cfg.Addr, &tls.Config{ServerName: hostOnly(cfg.Addr)})
	} else {
		c, err = client.Dial(cfg.Addr)
	}
	if err != nil {
		return nil, fmt.Errorf("imap dial %s: %w", cfg.Addr, err)
	}

	// Если сервер поддерживает STARTTLS и мы не подключились сразу по TLS —
	// апгрейдим соединение. Многие корпоративные сервера (в т.ч. Exchange)
	// требуют этого на 587/143.
	if !cfg.UseTLS {
		if ok, _ := c.SupportStartTLS(); ok {
			if err := c.StartTLS(&tls.Config{ServerName: hostOnly(cfg.Addr)}); err != nil {
				return nil, fmt.Errorf("starttls %s: %w", cfg.Addr, err)
			}
		}
	}

	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		return nil, fmt.Errorf("imap login %s@%s: %w", cfg.Username, cfg.Addr, err)
	}

	return c, nil
}

func hostOnly(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
