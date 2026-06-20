// Command rsvdata-bridge — локальный мост между MCP-клиентом (Claude, Cursor и др.)
// и базой 1С через COM (внешнее соединение). Не требует публикации HTTP-сервиса.
//
// Подкоманды:
//
//	rsvdata-bridge            — режим моста (stdio ↔ COM). Это запускает MCP-клиент.
//	rsvdata-bridge serve      — то же явно.
//	rsvdata-bridge setup      — мастер настройки: спросит базу/логин/пароль, проверит
//	                            связь и пропишет сервер в конфиг MCP-клиента.
//	rsvdata-bridge ping       — разовая проверка связи с базой (для диагностики).
//
// Параметры подключения берутся (по приоритету): флаг --connect → файл конфигурации
// (--config или путь по умолчанию). Логин/пароль хранятся в конфиге обычным текстом —
// аутентификация платформы 1С остаётся включённой (см. ../04-architecture-and-conventions.md §7).
package main

import (
	"fmt"
	"os"
)

func main() {
	// Подкоманда — первый аргумент без дефиса.
	// По умолчанию (нет аргументов — например, двойной щелчок в Проводнике) запускается мастер
	// настройки. MCP-клиент всегда запускает мост с аргументом "serve".
	cmd := "setup"
	args := os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "serve":
		runServe(args)
	case "setup":
		runSetup(args)
	case "ping":
		runPing(args)
	case "diag":
		runDiag(args)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "Неизвестная команда: %s\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `MCP:RSV Data — мост stdio ↔ 1С (COM).

Команды:
  setup    Мастер настройки (по умолчанию — запускается двойным щелчком): способ
           подключения, база, логин, пароль; проверка связи; готовый блок .mcp.json
           (сохраняется в файл и в буфер обмена).
  serve    Режим моста. Запускается MCP-клиентом, читает JSON-RPC из stdin,
           отвечает в stdout.
  ping     Разовая проверка связи с базой 1С через COM (диагностика).
  diag     Диагностика базы: список расширений + доступность модулей RSVData
           через внешнее соединение. Помогает понять, почему мост не видит RSVData.

Общие флаги:
  --config <путь>    Файл конфигурации (по умолчанию рядом с профилем пользователя).
  --connect "<стр>"  Полная строка соединения 1С (переопределяет конфиг).

Примеры:
  rsvdata-bridge setup
  rsvdata-bridge ping --config C:\Users\you\AppData\Roaming\MCP-RSV-Data\bridge.json
`)
}

// resolveConfig читает конфиг с учётом флагов --config / --connect.
func resolveConfig(configPath, connect string) (*Config, error) {
	if connect != "" {
		return &Config{Connect: connect}, nil
	}
	path := configPath
	if path == "" {
		path = defaultConfigPath()
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать конфиг %s: %w (запустите «rsvdata-bridge setup»)", path, err)
	}
	return cfg, nil
}
