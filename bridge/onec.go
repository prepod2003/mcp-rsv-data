package main

import (
	"fmt"
	"runtime"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// Имена COM-объекта 1С и точки входа диспетчера MCP в расширении RSVData.
const (
	progIDConnector  = "V83.COMConnector"    // COM-соединитель платформы 1С 8.3
	dispatcherModule = "RSVData_Сервер"      // общий модуль (флаг «Внешнее соединение»)
	dispatcherFunc   = "ОбработатьСообщение" // Функция(ТекстСообщения) Экспорт → JSON-ответ
)

// OneC — соединение с базой 1С через COM. Всё взаимодействие с COM идёт в одном
// выделенном потоке ОС (требование COM: объекты используются в потоке, где вызван
// CoInitialize). Операции передаются через канал как функции над соединением.
type OneC struct {
	reqCh chan comReq
}

type comReq struct {
	fn    func(conn *ole.IDispatch) (string, error)
	reply chan comResp
}

type comResp struct {
	out string
	err error
}

// NewOneC создаёт COM-соединитель, подключается к базе и держит соединение открытым.
func NewOneC(connectStr string) (*OneC, error) {
	o := &OneC{reqCh: make(chan comReq)}
	ready := make(chan error, 1)
	go o.loop(connectStr, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return o, nil
}

// run выполняет функцию над соединением в COM-потоке и возвращает результат.
func (o *OneC) run(fn func(conn *ole.IDispatch) (string, error)) (string, error) {
	reply := make(chan comResp, 1)
	o.reqCh <- comReq{fn: fn, reply: reply}
	r := <-reply
	return r.out, r.err
}

// Process отправляет одно MCP-сообщение в 1С и возвращает ответ-строку (может быть пустой).
func (o *OneC) Process(msg string) (string, error) {
	return o.run(func(conn *ole.IDispatch) (string, error) {
		return callDispatcher(conn, msg)
	})
}

// Diag возвращает диагностический отчёт по соединению (расширения + доступность модулей).
func (o *OneC) Diag() (string, error) {
	return o.run(diagReport)
}

// Close завершает поток COM.
func (o *OneC) Close() {
	close(o.reqCh)
}

func (o *OneC) loop(connectStr string, ready chan error) {
	// COM требует, чтобы объекты использовались в том же потоке ОС, что и CoInitialize.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		ready <- fmt.Errorf("CoInitializeEx: %w", err)
		return
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject(progIDConnector)
	if err != nil {
		ready <- fmt.Errorf("не удалось создать %s — установлена ли платформа 1С и зарегистрирован ли COM-соединитель (regsvr32 comcntr.dll)? %w", progIDConnector, err)
		return
	}
	defer unknown.Release()

	connector, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		ready <- fmt.Errorf("QueryInterface COMConnector: %w", err)
		return
	}
	defer connector.Release()

	connVar, err := oleutil.CallMethod(connector, "Connect", connectStr)
	if err != nil {
		ready <- fmt.Errorf("не удалось подключиться к базе 1С (проверьте путь/сервер, логин и пароль): %w", err)
		return
	}
	conn := connVar.ToIDispatch()
	defer conn.Release()

	ready <- nil

	for req := range o.reqCh {
		out, err := req.fn(conn)
		req.reply <- comResp{out: out, err: err}
	}
}

// callDispatcher вызывает RSVData_Сервер.ОбработатьСообщение(текст) во внешнем соединении.
func callDispatcher(conn *ole.IDispatch, msg string) (string, error) {
	modVar, err := oleutil.GetProperty(conn, dispatcherModule)
	if err != nil {
		return "", fmt.Errorf("нет общего модуля %s (установлено ли расширение RSVData и есть ли у модуля флаг «Внешнее соединение»?): %w", dispatcherModule, err)
	}
	module := modVar.ToIDispatch()
	defer module.Release()

	resVar, err := oleutil.CallMethod(module, dispatcherFunc, msg)
	if err != nil {
		return "", fmt.Errorf("%s.%s: %w", dispatcherModule, dispatcherFunc, err)
	}
	defer resVar.Clear()
	return resVar.ToString(), nil
}
