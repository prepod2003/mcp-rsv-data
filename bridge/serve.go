package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// runServe — режим моста. Читает построчно JSON-RPC из stdin, прокачивает в 1С через
// COM и пишет компактные JSON-ответы в stdout (по одному на строку, как требует
// stdio-транспорт MCP). Диагностика идёт в stderr, чтобы не мешать протоколу.
func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "путь к файлу конфигурации")
	connect := fs.String("connect", "", "полная строка соединения 1С (переопределяет конфиг)")
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
	fmt.Fprintln(os.Stderr, "rsvdata-bridge: соединение с 1С установлено, мост готов")

	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)

	for {
		line, readErr := reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			handleLine(onec, trimmed, writer)
		}
		if readErr != nil {
			if readErr != io.EOF {
				fmt.Fprintln(os.Stderr, "rsvdata-bridge: чтение stdin:", readErr)
			}
			return
		}
	}
}

func handleLine(onec *OneC, reqLine []byte, writer *bufio.Writer) {
	resp, err := onec.Process(string(reqLine))
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsvdata-bridge:", err)
		resp = errorResponse(extractID(reqLine), -32000, "Мост 1С: "+err.Error())
	}
	if strings.TrimSpace(resp) == "" {
		return // уведомление — ответа нет
	}
	writeResponse(writer, resp)
}

// writeResponse пишет ответ одной строкой. JSON компактируется (json.Compact убирает
// форматирующие переводы строк, не трогая содержимое) — иначе многострочный JSON от 1С
// сломал бы построчное кадрирование stdio-транспорта.
func writeResponse(writer *bufio.Writer, resp string) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(resp)); err == nil {
		writer.Write(buf.Bytes())
	} else {
		// На всякий случай: вычистим переводы строк вручную.
		writer.WriteString(strings.NewReplacer("\r", "", "\n", "").Replace(resp))
	}
	writer.WriteByte('\n')
	writer.Flush()
}

// extractID достаёт id входящего JSON-RPC запроса (для корректного ответа об ошибке).
func extractID(reqLine []byte) json.RawMessage {
	var m struct {
		ID json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(reqLine, &m)
	return m.ID
}

func errorResponse(id json.RawMessage, code int, message string) string {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	payload := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{JSONRPC: "2.0", ID: id}
	payload.Error.Code = code
	payload.Error.Message = message
	b, _ := json.Marshal(payload)
	return string(b)
}
