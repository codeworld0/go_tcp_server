package rltcpkit

// Logger определяет интерфейс для логгирования событий TCP сервера.
// Реализация этого интерфейса может быть передана серверу для логгирования
// различных событий: подключений, ошибок, отключений и других важных операций.
type Logger interface {
	// Info логгирует информационное сообщение с опциональными аргументами.
	// Аргументы форматируются в стиле fmt.Printf.
	Info(msg string, args ...interface{})

	// Warn логгирует предупреждающее сообщение с опциональными аргументами.
	// Используется для нештатных ситуаций, не требующих немедленного вмешательства.
	Warn(msg string, args ...interface{})

	// Error логгирует сообщение об ошибке с опциональными аргументами.
	// Используется для критических ошибок, требующих внимания.
	Error(msg string, args ...interface{})
}

// noopLogger реализует Logger интерфейс, но не выполняет никаких действий.
// Используется как дефолтный логгер, если пользователь не предоставил свой.
type noopLogger struct{}

func (n *noopLogger) Info(msg string, args ...interface{})  {}
func (n *noopLogger) Warn(msg string, args ...interface{})  {}
func (n *noopLogger) Error(msg string, args ...interface{}) {}

// NewNoopLogger создает новый логгер, который игнорирует все сообщения.
// Полезно для тестирования или когда логгирование не требуется.
func NewNoopLogger() Logger {
	return &noopLogger{}
}
