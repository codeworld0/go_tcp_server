package rltcpkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrServerNotStarted возвращается при попытке остановить незапущенный сервер
	ErrServerNotStarted = errors.New("server not started")

	// ErrServerAlreadyStarted возвращается при попытке запустить уже работающий сервер
	ErrServerAlreadyStarted = errors.New("server already started")

	// ErrMaxConnectionsReached возвращается когда достигнут лимит подключений
	ErrMaxConnectionsReached = errors.New("maximum connections reached")
)

// Config содержит параметры конфигурации TCP сервера.
type Config struct {
	// MaxConnections ограничивает максимальное количество одновременных подключений.
	// 0 или отрицательное значение означает отсутствие ограничения.
	MaxConnections int

	// Logger используется для логгирования событий сервера.
	// Если nil, используется slog.Logger с выводом в io.Discard (без логгирования).
	Logger *slog.Logger

	// LogLevel определяет уровень детализации debug логов.
	// Не влияет на Info/Warn/Error логи - они всегда выводятся.
	// По умолчанию LogLevelInfo (debug логи отключены).
	LogLevel LogLevel

	// ReadBufferSize размер буфера чтения для каждого соединения (в байтах).
	// Если 0, используется значение по умолчанию из системы.
	ReadBufferSize int

	// WriteBufferSize размер буфера записи для каждого соединения (в байтах).
	// Если 0, используется значение по умолчанию из системы.
	WriteBufferSize int
}

// Server представляет TCP сервер с поддержкой generic протоколов.
// Параметр типа T определяет тип пакетов данных, используемых в протоколе.
//
// Сервер управляет жизненным циклом соединений, поддерживает graceful shutdown
// и предоставляет гибкую систему обработчиков событий.
type Server[T any] struct {
	// Конфигурация
	address string
	config  Config

	// Состояние сервера
	listener        net.Listener
	running         atomic.Bool
	ctx             context.Context
	cancel          context.CancelFunc
	startOnce       sync.Once
	stopOnce        sync.Once
	gracefulTimeout time.Duration
	done            chan struct{}

	// Управление соединениями
	connections sync.Map       // map[*Connection[T]]struct{}
	acceptWg    sync.WaitGroup // для ожидания завершения acceptLoop
	connWg      sync.WaitGroup // для ожидания завершения всех соединений
	connCount   atomic.Int64   // счётчик для GetConnectionCount()

	// Обработчики
	onAccept func(*Connection[T]) ConnectionHandlers[T]
	parser   ProtocolParser[T]

	// Logger
	logger *slog.Logger
}

// NewServer создает новый TCP сервер с указанными адресом и конфигурацией.
//
// Параметры:
//   - address: адрес для прослушивания в формате "host:port" (например, "0.0.0.0:8080" или ":8080")
//   - config: конфигурация сервера
//
// Возвращает:
//   - Новый экземпляр сервера
//
// Пример:
//
//	server := NewServer[[]byte](":8080", Config{
//	    MaxConnections: 1000,
//	    Logger: myLogger,
//	})
func NewServer[T any](address string, config Config) *Server[T] {
	if config.Logger == nil {
		config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &Server[T]{
		address: address,
		config:  config,
		logger:  config.Logger.With("pkg", "rltcpkit.server"),
	}
}

// SetGracefulTimeout устанавливает таймаут для graceful shutdown.
//
// Параметры:
//   - timeout: время ожидания graceful shutdown
//   - > 0: при остановке сервера будет вызван OnStop для всех соединений и ожидание указанное время
//   - == 0: немедленное закрытие всех соединений без вызова OnStop
//
// Метод может быть вызван в любое время, в том числе до запуска сервера.
//
// Пример:
//
//	server.SetGracefulTimeout(5 * time.Second)
func (s *Server[T]) SetGracefulTimeout(timeout time.Duration) {
	s.gracefulTimeout = timeout
}

// Start запускает TCP сервер и начинает принимать подключения.
//
// Параметры:
//   - ctx: контекст для управления жизненным циклом сервера (при завершении сервер автоматически останавливается)
//   - parser: парсер протокола для чтения и записи пакетов
//   - onAccept: функция, вызываемая при новом подключении для получения обработчиков
//
// Возвращает:
//   - <-chan struct{}: канал, который закрывается при полной остановке сервера
//   - error: ошибка запуска или nil при успехе
//
// Метод создает listener и запускает горутину для принятия подключений.
// Для каждого нового подключения вызывается onAccept, который должен вернуть
// набор обработчиков для этого соединения.
//
// При завершении переданного контекста, сервер автоматически вызывает Stop()
// с использованием graceful shutdown timeout (установленного через SetGracefulTimeout).
//
// Пример:
//
//	done, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
//	    return ConnectionHandlers[[]byte]{
//	        OnRead: func(ctx context.Context, c *Connection[[]byte], data []byte) {
//	            c.Write(data) // echo
//	        },
//	    }
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	<-done // ждем полной остановки сервера
func (s *Server[T]) Start(
	ctx context.Context,
	parser ProtocolParser[T],
	onAccept func(*Connection[T]) ConnectionHandlers[T],
) (<-chan struct{}, error) {
	if s.running.Load() {
		return nil, ErrServerAlreadyStarted
	}

	var startErr error
	s.startOnce.Do(func() {
		s.parser = parser
		s.onAccept = onAccept

		// Создаем дочерний контекст
		s.ctx, s.cancel = context.WithCancel(ctx)

		// Инициализируем канал done
		s.done = make(chan struct{})

		// Создаем listener
		listener, err := net.Listen("tcp", s.address)
		if err != nil {
			startErr = fmt.Errorf("failed to start listener: %w", err)
			return
		}

		s.listener = listener
		s.running.Store(true)

		s.logger.Info("TCP server started", "address", s.address)

		// Запускаем горутину для accept
		s.acceptWg.Add(1)
		go s.acceptLoop()

		// Запускаем монитор контекста для автоматической остановки
		go s.contextMonitor()
	})

	if startErr != nil {
		return nil, startErr
	}

	return s.done, nil
}

// acceptLoop принимает новые подключения в отдельной горутине.
func (s *Server[T]) acceptLoop() {
	defer s.acceptWg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			// Проверяем, не закрыт ли listener
			if errors.Is(err, net.ErrClosed) {
				// Listener закрыт - выходим из цикла
				return
			}

			// Проверяем, не закрыт ли сервер через контекст
			select {
			case <-s.ctx.Done():
				return
			default:
				s.logger.Error("Accept error", "error", err)
				continue
			}
		}

		// Проверяем лимит подключений
		if s.config.MaxConnections > 0 {
			currentCount := s.connCount.Load()
			if currentCount >= int64(s.config.MaxConnections) {
				s.logger.Warn("Max connections reached, rejecting connection", "remote_addr", conn.RemoteAddr())
				conn.Close()
				continue
			}
		}

		s.logger.Info("New connection", "remote_addr", conn.RemoteAddr())

		// Обрабатываем новое подключение
		s.handleConnection(conn)
	}
}

// contextMonitor отслеживает завершение контекста и автоматически останавливает сервер.
func (s *Server[T]) contextMonitor() {
	<-s.ctx.Done()
	s.logger.Info("Context cancelled, stopping server")
	_ = s.Stop()
}

// handleConnection обрабатывает новое подключение.
func (s *Server[T]) handleConnection(conn net.Conn) {
	// Увеличиваем WaitGroup и счетчик подключений
	s.connWg.Add(1)
	s.connCount.Add(1)

	// Создаем временные обработчики для получения реальных от onAccept
	var handlers ConnectionHandlers[T]

	// Объявляем переменную для connection
	var connection *Connection[T]

	// Создаем cleanup функцию, которая будет вызвана когда eventLoop завершится
	cleanupFunc := func() {
		// Удаляем из карты активных соединений
		s.connections.Delete(connection)

		// Уменьшаем счетчик подключений
		s.connCount.Add(-1)

		// Уведомляем WaitGroup о завершении соединения
		s.connWg.Done()

		s.logger.Info("Connection closed", "conn_id", connection.id, "remote_addr", conn.RemoteAddr())
	}

	// Создаем объект Connection
	connection = newConnection(
		conn,
		s.parser,
		handlers,
		s.logger,
		s.config.LogLevel,
		s.ctx,
		cleanupFunc,
	)

	// Добавляем соединение в карту активных соединений
	s.connections.Store(connection, struct{}{})

	// Вызываем onAccept для получения обработчиков
	if s.onAccept != nil {
		handlers = s.onAccept(connection)
		connection.SetHandlers(handlers)
	}
}

// Stop останавливает TCP сервер с graceful shutdown.
//
// Возвращает:
//   - error: ошибка остановки или nil при успехе
//
// Процесс остановки:
//  1. Закрывает listener (новые подключения не принимаются)
//  2. Если gracefulTimeout > 0: отправляет сигнал shutdown всем соединениям (вызывает OnStop)
//  3. Ждет gracefulTimeout или пока все соединения не закроются
//  4. Принудительно закрывает все оставшиеся соединения
//  5. Для каждого закрытого соединения вызывается OnClosed
//  6. Закрывает канал done для уведомления о полной остановке
//
// Таймаут graceful shutdown устанавливается через SetGracefulTimeout.
// Если таймаут равен 0, соединения закрываются немедленно без graceful shutdown.
//
// Метод потокобезопасен и может быть вызван многократно.
//
// Пример:
//
//	// Установить таймаут и остановить сервер
//	server.SetGracefulTimeout(5 * time.Second)
//	server.Stop()
func (s *Server[T]) Stop() error {
	if !s.running.Load() {
		return ErrServerNotStarted
	}

	var stopErr error
	s.stopOnce.Do(func() {
		s.logger.Info("Stopping TCP server")

		// Отменяем контекст сервера первым делом, чтобы acceptLoop корректно завершился
		s.cancel()

		// Закрываем listener, чтобы не принимать новые подключения
		if s.listener != nil {
			if err := s.listener.Close(); err != nil {
				s.logger.Error("Error closing listener", "error", err)
				stopErr = err
			}
		}
		// Ждем завершения accept loop
		s.acceptWg.Wait()
		// Если gracefulTimeout > 0, выполняем graceful shutdown
		if s.gracefulTimeout > 0 {
			s.logger.Info("Starting graceful shutdown", "timeout", s.gracefulTimeout)

			// Уведомляем все соединения о shutdown
			s.connections.Range(func(key, value interface{}) bool {
				if conn, ok := key.(*Connection[T]); ok {
					conn.NotifyShutdown()
				}
				return true
			})

			// Ждем gracefulTimeout или пока все соединения не закроются
			done := make(chan struct{})
			go func() {
				s.connWg.Wait()
				close(done)
			}()

			select {
			case <-done:
				s.logger.Info("All connections closed gracefully")
			case <-time.After(s.gracefulTimeout):
				s.logger.Warn("Graceful shutdown timeout, forcefully closing remaining connections")
			}
		}

		// Принудительно закрываем все оставшиеся соединения
		s.connections.Range(func(key, value interface{}) bool {
			if conn, ok := key.(*Connection[T]); ok {
				conn.Close(true)
			}
			return true
		})

		// Ждем завершения всех соединений (важно даже если gracefulTimeout == 0)
		s.connWg.Wait()

		s.running.Store(false)
		s.logger.Info("TCP server stopped")

		// Закрываем канал done для уведомления о полной остановке
		if s.done != nil {
			close(s.done)
		}
	})

	return stopErr
}

// GetConnectionCount возвращает текущее количество активных подключений.
func (s *Server[T]) GetConnectionCount() int64 {
	return s.connCount.Load()
}

// IsRunning возвращает true, если сервер запущен.
func (s *Server[T]) IsRunning() bool {
	return s.running.Load()
}

// GetAddress возвращает адрес, на котором работает сервер.
func (s *Server[T]) GetAddress() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.address
}

// ForEachConnection выполняет функцию fn для каждого активного соединения.
// Если fn возвращает false, обход прерывается.
//
// Метод потокобезопасен и может быть вызван во время работы сервера.
func (s *Server[T]) ForEachConnection(fn func(*Connection[T]) bool) {
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*Connection[T]); ok {
			return fn(conn)
		}
		return true
	})
}
