package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// runDiag — диагностика подключения к базе: какие расширения установлены/активны и
// доступны ли через внешнее соединение нужные общие модули. Помогает понять, почему
// мост не видит RSVData в конкретной базе.
//
//	rsvdata-bridge diag
//	rsvdata-bridge diag --connect 'File="D:\Базы\МояБаза";Usr="Администратор";Pwd="";'
func runDiag(args []string) {
	fs := flag.NewFlagSet("diag", flag.ExitOnError)
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

	report, err := onec.Diag()
	if err != nil {
		fmt.Fprintln(os.Stderr, "rsvdata-bridge: диагностика не удалась:", err)
		os.Exit(1)
	}
	fmt.Println(report)
}

// diagReport собирает отчёт по уже открытому соединению.
func diagReport(conn *ole.IDispatch) (string, error) {
	var sb strings.Builder
	sb.WriteString("Соединение с базой: установлено.\n\n")

	sb.WriteString("Расширения конфигурации в базе:\n")
	sb.WriteString(listExtensions(conn))
	sb.WriteString("\n")

	sb.WriteString("Доступность общих модулей через внешнее соединение:\n")
	for _, name := range []string{"RSVData_Сервер", "RSVData_Ядро", "RSVData_Справка", "ОбщегоНазначения"} {
		if _, err := oleutil.GetProperty(conn, name); err != nil {
			sb.WriteString(fmt.Sprintf("  - %s: НЕ доступен (%v)\n", name, err))
		} else {
			sb.WriteString(fmt.Sprintf("  - %s: доступен\n", name))
		}
	}
	return sb.String(), nil
}

// listExtensions перечисляет расширения через РасширенияКонфигурации.Получить().
// Обёрнуто защитно: при любой проблеме с COM возвращает строку-пояснение, а не падает.
func listExtensions(conn *ole.IDispatch) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = fmt.Sprintf("  (не удалось получить список: %v)\n", r)
		}
	}()

	mgrVar, err := oleutil.GetProperty(conn, "РасширенияКонфигурации")
	if err != nil {
		return fmt.Sprintf("  (РасширенияКонфигурации недоступно: %v)\n", err)
	}
	mgr := mgrVar.ToIDispatch()
	defer mgr.Release()

	arrVar, err := oleutil.CallMethod(mgr, "Получить")
	if err != nil {
		return fmt.Sprintf("  (Получить() не выполнен: %v)\n", err)
	}
	arr := arrVar.ToIDispatch()
	defer arr.Release()

	cntVar, err := oleutil.CallMethod(arr, "Количество")
	if err != nil {
		return fmt.Sprintf("  (Количество() не выполнен: %v)\n", err)
	}
	n := variantToInt(cntVar)
	if n == 0 {
		return "  (расширений нет)\n"
	}

	var sb strings.Builder
	for i := 0; i < n; i++ {
		eVar, err := oleutil.CallMethod(arr, "Получить", i)
		if err != nil {
			continue
		}
		e := eVar.ToIDispatch()
		name := getStrProp(e, "Имя")
		active := getProp(e, "Активно")
		sb.WriteString(fmt.Sprintf("  - %s (активно=%v)\n", name, active))
		e.Release()
	}
	return sb.String()
}

func getStrProp(d *ole.IDispatch, name string) string {
	v, err := oleutil.GetProperty(d, name)
	if err != nil {
		return "?"
	}
	return v.ToString()
}

func getProp(d *ole.IDispatch, name string) any {
	v, err := oleutil.GetProperty(d, name)
	if err != nil {
		return "?"
	}
	return v.Value()
}

func variantToInt(v *ole.VARIANT) int {
	switch x := v.Value().(type) {
	case int8:
		return int(x)
	case int16:
		return int(x)
	case int32:
		return int(x)
	case int64:
		return int(x)
	case int:
		return x
	case float32:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}
