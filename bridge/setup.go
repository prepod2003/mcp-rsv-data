package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runPing — разовая проверка связи через COM (диагностика для режима моста).
//
//	rsvdata-bridge ping
//	rsvdata-bridge ping --method tools/list
//	rsvdata-bridge ping --connect 'File="..."' --method ping
func runPing(args []string) {
	fs := flag.NewFlagSet("ping", flag.ExitOnError)
	configPath := fs.String("config", "", "путь к файлу конфигурации")
	connect := fs.String("connect", "", "полная строка соединения 1С (переопределяет конфиг)")
	method := fs.String("method", "ping", "MCP-метод (ping, tools/list, initialize, tools/call)")
	params := fs.String("params", "", "JSON параметров (для tools/call)")
	_ = fs.Parse(args)

	cfg, err := resolveConfig(*configPath, *connect)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsvdata-bridge:", err)
		os.Exit(1)
	}

	onec, err := NewOneC(cfg.ConnectString())
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsvdata-bridge: подключение к 1С не удалось:", err)
		os.Exit(1)
	}
	defer onec.Close()

	req := buildRequest(*method, *params)
	resp, err := onec.Process(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsvdata-bridge: вызов не удался:", err)
		os.Exit(1)
	}
	fmt.Println(resp)
}

func buildRequest(method, params string) string {
	m := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if strings.TrimSpace(params) != "" {
		var p any
		if err := json.Unmarshal([]byte(params), &p); err == nil {
			m["params"] = p
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

const pingRequest = `{"jsonrpc":"2.0","id":1,"method":"ping"}`

// runSetup — единый мастер настройки. Рассчитан на запуск ДВОЙНЫМ ЩЕЛЧКОМ (без командной
// строки): открывается окно, мастер задаёт вопросы, в конце сохраняет готовый блок в файл,
// копирует его в буфер обмена и ждёт нажатия Enter, чтобы окно не закрылось.
// Никаких ручных действий (base64/PowerShell) от пользователя не требуется.
func runSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	configPath := fs.String("config", "", "куда сохранить конфиг моста (по умолчанию профиль пользователя)")
	_ = fs.Parse(args)

	in := bufio.NewReader(os.Stdin)
	defer pause(in) // окно остаётся открытым на любом исходе

	fmt.Println("=== Настройка MCP:RSV Data ===")
	fmt.Println("Подключим ИИ-клиент (Claude, Cursor) к данным вашей базы 1С.")
	fmt.Println()
	fmt.Println("Способы подключения:")
	fmt.Println("  [1] HTTP-сервис 1С — база опубликована на веб-сервере.")
	fmt.Println("  [2] Локальный мост — напрямую к базе (COM), без публикации (Windows).")
	method := askChoice(in, "Выберите способ (1 или 2)", []string{"1", "2"})
	fmt.Println()

	fmt.Println("Под каким именем добавить сервер в ИИ-клиент?")
	fmt.Println("Для НЕСКОЛЬКИХ баз запустите мастер по разу на каждую и задайте РАЗНЫЕ имена")
	fmt.Println("(например rsv-data, rsv-data-erp) — тогда базы не путаются и работают параллельно.")
	name := ask(in, "Имя подключения", "rsv-data")
	fmt.Println()

	if method == "1" {
		setupHTTP(in, name)
	} else {
		setupBridge(in, name, *configPath)
	}
}

// setupHTTP — способ 1 (HTTP-сервис). Спрашивает адрес/логин/пароль, сам собирает заголовок
// авторизации, проверяет связь и выдаёт готовый блок .mcp.json. При ошибке предлагает повтор
// БЕЗ повторного ввода тех же данных.
func setupHTTP(in *bufio.Reader, name string) {
	fmt.Println("--- Подключение через HTTP-сервис 1С ---")
	fmt.Println("У разных ИИ-клиентов формат HTTP-настройки отличается (тип транспорта).")
	client := askChoice(in, "Для какого клиента настраиваем? [1] Claude Code / VS Code, [2] Cursor, [3] Cline", []string{"1", "2", "3"})
	fmt.Println()
	fmt.Println("Нужен адрес опубликованного сервиса, обычно такой:")
	fmt.Println("  http://<веб-сервер>/<имя публикации>/hs/rsvdata/mcp")
	url := ask(in, "Адрес HTTP-сервиса", "http://localhost/UT_Demo/hs/rsvdata/mcp")
	user := ask(in, "Пользователь ИБ (логин)", "")
	password := ask(in, "Пароль (Enter — пустой)", "")
	auth := buildBasicAuth(user, password)

	for {
		fmt.Println()
		fmt.Println("Проверяю связь с сервисом…")
		resp, err := testHTTP(url, auth)
		if err == nil && strings.Contains(resp, "result") {
			fmt.Println("✓ Связь есть, расширение RSVData отвечает.")
			break
		}
		if err != nil {
			fmt.Println("✗ Не удалось проверить связь:", err)
			fmt.Println("  Возможно: база не опубликована, неверный адрес/логин/пароль или не включены")
			fmt.Println("  HTTP-сервисы расширения при публикации.")
		} else {
			fmt.Println("✗ Сервис ответил неожиданно (установлено ли расширение RSVData?):", strings.TrimSpace(resp))
		}
		switch askRetry(in) {
		case retrySame:
			continue
		case retryReenter:
			url = ask(in, "Адрес HTTP-сервиса", url)
			user = ask(in, "Пользователь ИБ (логин)", user)
			password = ask(in, "Пароль (Enter — пустой)", "")
			auth = buildBasicAuth(user, password)
			continue
		default: // выход: всё равно отдадим блок — вдруг сервис поднимут позже
			fmt.Println("Связь не проверена; блок ниже сформирован на введённых данных.")
		}
		break
	}

	deliverSnippet(in, name, httpServer(client, url, auth))
}

// httpServer строит описание HTTP-сервера в формате выбранного клиента.
// Тип транспорта у клиентов разный: Claude Code/VS Code — "http", Cline — "streamableHttp",
// Cursor — "streamable-http". Заголовки авторизации поддерживают все три.
func httpServer(client, url, auth string) map[string]any {
	headers := map[string]any{"Authorization": auth}
	switch client {
	case "2": // Cursor
		return map[string]any{"type": "streamable-http", "url": url, "headers": headers}
	case "3": // Cline
		return map[string]any{"type": "streamableHttp", "url": url, "headers": headers, "disabled": false}
	default: // Claude Code / VS Code
		return map[string]any{"type": "http", "url": url, "headers": headers}
	}
}

// setupBridge — способ 2 (локальный мост через COM). При ошибке предлагает повтор
// (теми же данными или с повторным вводом) — окно не закрывается.
func setupBridge(in *bufio.Reader, name, configPath string) {
	fmt.Println("--- Подключение через локальный мост (COM) ---")
	fmt.Println("Веб-публикация не нужна. Логин/пароль сохранятся локально в вашем профиле.")
	fmt.Println()

	cfg := askBridgeParams(in, nil)

	for {
		fmt.Println()
		fmt.Println("Проверяю связь с базой…")
		err := tryBridge(cfg)
		if err == nil {
			fmt.Println("✓ Связь с базой есть, расширение RSVData отвечает.")
			break
		}
		fmt.Println("✗ Подключиться не удалось:", err)
		fmt.Println("  Подсказка: для диагностики (какие расширения видны в базе) запустите")
		fmt.Println("  rsvdata-bridge.exe diag — он покажет, активно ли расширение RSVData.")
		switch askRetry(in) {
		case retrySame:
			continue // те же параметры — например, вы обновили конфигурацию базы и пробуете снова
		case retryReenter:
			cfg = askBridgeParams(in, cfg)
			continue
		default:
			return // выход без сохранения
		}
	}

	path := configPath
	if path == "" {
		path = namedConfigPath(name)
	}
	if err := saveConfig(path, cfg); err != nil {
		fmt.Println("✗ Не удалось сохранить настройки:", err)
		return
	}
	fmt.Println("✓ Параметры подключения (база, логин, пароль) сохранены в файл:")
	fmt.Println("   ", path)

	exe, _ := os.Executable()
	deliverSnippet(in, name, map[string]any{
		"type":    "stdio",
		"command": exe,
		"args":    []string{"serve", "--config", path},
	})
}

// askBridgeParams спрашивает параметры файловой/серверной базы. prev (если задан) —
// значения по умолчанию при повторном вводе.
func askBridgeParams(in *bufio.Reader, prev *Config) *Config {
	cfg := &Config{}
	defKind := "1"
	if prev != nil && prev.Kind == "server" {
		defKind = "2"
	}
	kind := askChoiceDefault(in, "Тип базы — [1] файловая, [2] клиент-серверная", []string{"1", "2"}, defKind)
	if kind == "2" {
		cfg.Kind = "server"
		ds, dr := "localhost", ""
		if prev != nil {
			if prev.Server != "" {
				ds = prev.Server
			}
			dr = prev.Ref
		}
		cfg.Server = ask(in, "Адрес сервера 1С (например, localhost или srv:1541)", ds)
		cfg.Ref = ask(in, "Имя базы на сервере", dr)
	} else {
		cfg.Kind = "file"
		df := ""
		if prev != nil {
			df = prev.File
		}
		cfg.File = ask(in, `Путь к папке файловой базы (например, D:\Базы\МояБаза)`, df)
	}
	du := ""
	if prev != nil {
		du = prev.User
	}
	cfg.User = ask(in, "Пользователь ИБ (логин)", du)
	cfg.Password = ask(in, "Пароль (Enter — пустой)", "")
	return cfg
}

// tryBridge открывает COM-соединение, шлёт ping и проверяет ответ.
func tryBridge(cfg *Config) error {
	onec, err := NewOneC(cfg.ConnectString())
	if err != nil {
		return err
	}
	defer onec.Close()
	resp, err := onec.Process(pingRequest)
	if err != nil {
		return err
	}
	if !strings.Contains(resp, "result") {
		return fmt.Errorf("база ответила без result: %s", strings.TrimSpace(resp))
	}
	return nil
}

type retryChoice int

const (
	retrySame retryChoice = iota
	retryReenter
	retryQuit
)

// askRetry предлагает действия после неудачной попытки — без повторного ввода тех же данных.
func askRetry(in *bufio.Reader) retryChoice {
	fmt.Println()
	switch askChoice(in, "Что дальше — [1] повторить с теми же данными, [2] ввести заново, [3] выход", []string{"1", "2", "3"}) {
	case "1":
		return retrySame
	case "2":
		return retryReenter
	default:
		return retryQuit
	}
}

// clientBlock оборачивает описание сервера в готовый фрагмент конфига MCP-клиента
// под заданным именем (ключ в mcpServers).
func clientBlock(name string, server map[string]any) string {
	b, _ := json.MarshalIndent(map[string]any{
		"mcpServers": map[string]any{name: server},
	}, "", "  ")
	return string(b)
}

// deliverSnippet показывает готовый блок и предлагает дописать сервер в конфиг клиента
// автоматически. При отказе — обычный путь: блок напечатан, сохранён в файл и в буфер обмена,
// пользователь вставляет сам.
func deliverSnippet(in *bufio.Reader, name string, server map[string]any) {
	block := clientBlock(name, server)

	fmt.Println()
	fmt.Println("=== Готово. Настройка для ИИ-клиента ===")
	fmt.Println()
	fmt.Println(block)
	fmt.Println()
	if path, err := saveSnippetFile(name, block); err == nil {
		fmt.Println("Этот блок сохранён в файл:", path)
	}
	if err := copyToClipboard(block); err == nil {
		fmt.Println("…и скопирован в буфер обмена.")
	}
	fmt.Println()

	choice := askChoice(in, "Дописать сервер в конфиг клиента автоматически? [1] да, [2] нет (вставлю сам)", []string{"1", "2"})
	if choice != "1" {
		fmt.Printf("Хорошо. Вставьте блок выше в файл .mcp.json вашего клиента (раздел mcpServers) и перезапустите клиент — сервер %s появится.\n", name)
		return
	}

	fmt.Println()
	fmt.Println("Где обычно лежит файл конфигурации клиента:")
	fmt.Println(`  Claude Code (проект):  .mcp.json в корне проекта`)
	fmt.Println(`  Cursor:                <папка профиля>\.cursor\mcp.json`)
	fmt.Println(`  Cline:                 ...\globalStorage\saoudrizwan.claude-dev\settings\cline_mcp_settings.json`)
	path := strings.Trim(strings.TrimSpace(ask(in, "Путь к файлу конфигурации клиента", "")), `"'`)
	if path == "" {
		fmt.Println("Путь не указан — авто-запись пропущена. Вставьте блок вручную (он в буфере обмена).")
		return
	}
	if err := mergeIntoClientConfig(path, name, server); err != nil {
		fmt.Println("✗ Не удалось дописать в конфиг:", err)
		fmt.Println("  Вставьте блок выше вручную (он в буфере обмена и в файле).")
		return
	}
	fmt.Printf("✓ Сервер %s добавлен в файл: %s\n", name, path)
	fmt.Println("Перезапустите ИИ-клиент — сервер появится в списке.")
}

// mergeIntoClientConfig аккуратно добавляет (или заменяет) запись сервера в конфиг клиента,
// сохраняя остальные серверы и прочие настройки. Если файла нет — создаёт. Старый файл бэкапит в .bak.
// Невалидный JSON не трогает (чтобы не повредить).
func mergeIntoClientConfig(path, name string, server map[string]any) error {
	var root map[string]any
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(strings.TrimSpace(string(data))) > 0 {
			if e := json.Unmarshal(data, &root); e != nil {
				return fmt.Errorf("файл не является корректным JSON (%v) — не трогаю его, чтобы не повредить; добавьте сервер вручную", e)
			}
		}
	case os.IsNotExist(err):
		// файла нет — создадим новый
	default:
		return err
	}
	if root == nil {
		root = map[string]any{}
	}

	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[name] = server
	root["mcpServers"] = servers

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 0 {
		_ = os.WriteFile(path+".bak", data, 0o644) // резервная копия прежнего файла
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

func saveSnippetFile(name, block string) (string, error) {
	fileName := "Настройка " + sanitizeFileName(name) + " (для mcp.json).txt"
	dir := ""
	if home, err := os.UserHomeDir(); err == nil {
		desktop := filepath.Join(home, "Desktop")
		if st, err := os.Stat(desktop); err == nil && st.IsDir() {
			dir = desktop
		} else {
			dir = home
		}
	}
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte(block+"\n"), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// copyToClipboard кладёт текст в буфер обмена через системный clip.exe (Windows).
func copyToClipboard(s string) error {
	cmd := exec.Command("clip.exe")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_, _ = io.WriteString(stdin, s)
	_ = stdin.Close()
	return cmd.Wait()
}

// buildBasicAuth собирает заголовок Basic-аутентификации из логина и пароля (UTF-8).
func buildBasicAuth(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// testHTTP шлёт ping в HTTP-сервис и проверяет ответ.
func testHTTP(url, auth string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(pingRequest))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", auth)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("HTTP 401 — неверный логин или пароль")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return string(data), nil
}

// pause держит окно открытым после завершения мастера (двойной щелчок).
func pause(in *bufio.Reader) {
	fmt.Print("\nНажмите Enter, чтобы закрыть окно… ")
	_, _ = in.ReadString('\n')
}

// readLine читает строку; eof=true, если ввод закрыт и строки больше нет.
func readLine(in *bufio.Reader) (line string, eof bool) {
	s, err := in.ReadString('\n')
	if err != nil && s == "" {
		return "", true
	}
	return strings.TrimRight(s, "\r\n"), false
}

// exitOnEOF завершает мастер, если стандартный ввод закрыт (чтобы не зацикливаться).
func exitOnEOF() {
	fmt.Fprintln(os.Stderr, "Ввод закрыт — мастер завершён.")
	os.Exit(1)
}

// ask печатает приглашение со значением по умолчанию и читает строку.
func ask(in *bufio.Reader, prompt, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", prompt, def)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	line, eof := readLine(in)
	if eof {
		exitOnEOF()
	}
	if strings.TrimSpace(line) == "" {
		return def
	}
	return line
}

// askChoice требует выбрать один из вариантов (без значения по умолчанию).
func askChoice(in *bufio.Reader, prompt string, allowed []string) string {
	return askChoiceDefault(in, prompt, allowed, "")
}

// askChoiceDefault — выбор из вариантов; при пустом вводе возвращает def (если задан и допустим).
func askChoiceDefault(in *bufio.Reader, prompt string, allowed []string, def string) string {
	label := prompt
	if def != "" {
		label = fmt.Sprintf("%s [%s]", prompt, def)
	}
	for {
		fmt.Printf("%s: ", label)
		raw, eof := readLine(in)
		if eof {
			exitOnEOF()
		}
		line := strings.TrimSpace(raw)
		if line == "" && def != "" {
			return def
		}
		for _, a := range allowed {
			if line == a {
				return line
			}
		}
		fmt.Printf("  Введите один из вариантов: %s\n", strings.Join(allowed, " / "))
	}
}
