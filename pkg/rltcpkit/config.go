package rltcpkit

// LogLevel определяет уровень детализации логирования для библиотеки rltcpkit.
// Уровни влияют только на debug логи - Info, Warn и Error логи всегда выводятся.
type LogLevel int

const (
	// LogLevelInfo отключает все debug логи, выводятся только Info/Warn/Error.
	// Подходит для production использования.
	LogLevelInfo LogLevel = 0

	// LogLevelDebug1 включает основные debug события:
	// - Закрытие соединений
	// - Context cancelled события
	// - Попытки reconnect и их результаты
	// - Connection cleanup
	LogLevelDebug1 LogLevel = 1

	// LogLevelDebug2 включает детали соединений (в дополнение к Debug1):
	// - EventLoop переходы между состояниями
	// - Запуск/остановка read/write горутин
	// - Вызовы обработчиков (OnRead, OnStop, OnError, OnConnected)
	// - Детали graceful shutdown процесса
	LogLevelDebug2 LogLevel = 2

	// LogLevelDebug3 включает максимальную детализацию (в дополнение к Debug1 и Debug2):
	// - Отправка пакетов с количеством байт
	// - Получение пакетов с количеством байт
	// - Состояния буферов каналов
	// - Низкоуровневые детали
	// ВНИМАНИЕ: Генерирует большой объем логов!
	LogLevelDebug3 LogLevel = 3
)

// String возвращает строковое представление уровня логирования.
func (l LogLevel) String() string {
	switch l {
	case LogLevelInfo:
		return "Info"
	case LogLevelDebug1:
		return "Debug1"
	case LogLevelDebug2:
		return "Debug2"
	case LogLevelDebug3:
		return "Debug3"
	default:
		return "Unknown"
	}
}
