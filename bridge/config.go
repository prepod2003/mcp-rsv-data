package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config — настройки подключения моста к базе 1С.
// Хранится в JSON. Логин/пароль — обычным текстом (аутентификация 1С включена).
type Config struct {
	// Connect — полная строка соединения 1С. Если задана, перекрывает структурированные поля.
	Connect string `json:"connect,omitempty"`

	// Структурированные поля (мастер setup заполняет их).
	Kind     string `json:"kind,omitempty"`   // "file" (файловая) или "server" (клиент-серверная)
	File     string `json:"file,omitempty"`   // путь к папке файловой базы
	Server   string `json:"server,omitempty"` // адрес сервера 1С (для server)
	Ref      string `json:"ref,omitempty"`    // имя базы на сервере (для server)
	User     string `json:"user,omitempty"`   // пользователь ИБ
	Password string `json:"password,omitempty"`
}

// ConnectString собирает строку соединения для V8*.COMConnector.Connect.
func (c *Config) ConnectString() string {
	if strings.TrimSpace(c.Connect) != "" {
		return c.Connect
	}
	var sb strings.Builder
	if c.Kind == "server" {
		fmt.Fprintf(&sb, `Srvr="%s";Ref="%s";`, c.Server, c.Ref)
	} else {
		fmt.Fprintf(&sb, `File="%s";`, c.File)
	}
	if strings.TrimSpace(c.User) != "" {
		fmt.Fprintf(&sb, `Usr="%s";Pwd="%s";`, c.User, c.Password)
	}
	return sb.String()
}

// configDir — папка с конфигами моста: %APPDATA%\MCP-RSV-Data (Windows).
func configDir() string {
	dir := os.Getenv("APPDATA")
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = home
		}
	}
	return filepath.Join(dir, "MCP-RSV-Data")
}

// defaultConfigPath — конфиг по умолчанию (если имя подключения не задано).
func defaultConfigPath() string {
	return filepath.Join(configDir(), "bridge.json")
}

// namedConfigPath — отдельный конфиг для подключения с данным именем. Так на одном мосте
// можно держать НЕСКОЛЬКО баз: каждому имени — свой файл, каждый MCP-сервер указывает на свой.
func namedConfigPath(name string) string {
	return filepath.Join(configDir(), sanitizeFileName(name)+".json")
}

// sanitizeFileName убирает из имени символы, недопустимые в имени файла Windows.
func sanitizeFileName(s string) string {
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}
	out := strings.Map(repl, strings.TrimSpace(s))
	if out == "" {
		return "rsv-data"
	}
	return out
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("неверный формат JSON: %w", err)
	}
	return &cfg, nil
}

func saveConfig(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 0600 — файл с кредами доступен только владельцу.
	return os.WriteFile(path, data, 0o600)
}
