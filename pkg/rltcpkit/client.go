package rltcpkit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrClientNotConnected возвращается при попытке использовать неподключенный клиент
	ErrClientNotConnected = errors.New("client not connected")

	// ErrClientNotStarted возвращается при попытке остановить незапущенный клиент
	ErrClientNotStarted = errors.New("client not started")

	// ErrClientAlreadyStarted возвращается при попытке запустить уже работающий клиент
	ErrClientAlreadyStarted = errors.New("client already started")

	// ErrClientDisconnecting возвращается при попытке операции во время отключения
	ErrClientDisconnecting = errors.New("client is disconnecting")

	// ErrReconnectFailed возвращается когда исчерпаны все попытки переподключения
	ErrReconnectFailed = errors.New("reconnect failed: max attempts reached")

	// ErrConnectionFailed возвращается при неудачной попытке подключения к серверу
	ErrConnectionFailed = errors.New("connection failed")
)

// ClientConfig содержит параметры конфигурации TCP клиента.
type ClientConfig struct {
	// ConnectTimeout таймаут для установки соединения.
	// Если 0, используется таймаут по умолчанию (10 секунд).
	ConnectTimeout time.Duration

	// ReconnectEnabled включает автоматическое переподключение при разрыве соединения.
	ReconnectEnabled bool

	// MaxReconnectAttempts максимальное количество попыток переподключения.
	// 0 означает бесконечное количество попыток.
	MaxReconnectAttempts int

	// ReconnectBaseDelay базовая задержка перед первой попыткой переподключения.
	// Задержка увеличивается экспоненциально: baseDelay * 2^(attempt-1).
	// Если 0, используется значение по умолчанию (1 секунда).
	ReconnectBaseDelay time.Duration

	// ReconnectMaxDelay максимальная задержка между попытками переподключения.
	// Если 0, используется значение по умолчанию (30 секунд).
	ReconnectMaxDelay time.Duration

	// Logger используется для логгирования событий клиента.
	// Если nil, используется NoopLogger (без логгирования).
	Logger Logger
}

// Client представляет TCP клиент с поддержкой автоматического переподключения.
// Параметр типа T определяет тип пакетов данных, используемых в протоколе.
type Client[T any] struct {
	// Конфигурация
	address string
	config  ClientConfig

	// Управление состоянием
	mu            sync.RWMutex
	conn          *Connection[T]
	connected     atomic.Bool
	disconnecting atomic.Bool
	running       atomic.Bool
	ctx           context.Context
	cancel        context.CancelFunc

	// Управление жизненным циклом
	startOnce       sync.Once
	stopOnce        sync.Once
	done            chan struct{}
	mainWg          sync.WaitGroup
	connWg          sync.WaitGroup // для ожидания завершения соединения
	gracefulTimeout time.Duration

	// Протокол и обработчики
	parser   ProtocolParser[T]
	handlers ClientHandlers[T]

	// Управление переподключением
	reconnectCh chan struct{} // канал для запуска переподключения

	// Logger
	logger Logger
}

// NewClient создает новый TCP клиент с указанными адресом и конфигурацией.
//
// Параметры:
//   - address: адрес сервера в формате "host:port" (например, "localhost:8080")
//   - config: конфигурация клиента
//
// Возвращает:
//   - Новый экземпляр клиента
//
// Пример:
//
//	client := NewClient[[]byte]("localhost:8080", ClientConfig{
//	    ConnectTimeout: 5 * time.Second,
//	    ReconnectEnabled: true,
//	    MaxReconnectAttempts: 5,
//	    ReconnectBaseDelay: 1 * time.Second,
//	    ReconnectMaxDelay: 30 * time.Second,
//	    Logger: myLogger,
//	})
func NewClient[T any](address string, config ClientConfig) *Client[T] {
	// Установка значений по умолчанию
	if config.Logger == nil {
		config.Logger = NewNoopLogger()
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 10 * time.Second
	}
	if config.ReconnectBaseDelay == 0 {
		config.ReconnectBaseDelay = 1 * time.Second
	}
	if config.ReconnectMaxDelay == 0 {
		config.ReconnectMaxDelay = 30 * time.Second
	}

	return &Client[T]{
		address:     address,
		config:      config,
		logger:      config.Logger,
		reconnectCh: make(chan struct{}, 1),
	}
}

// SetGracefulTimeout устанавливает таймаут для graceful shutdown.
//
// Параметры:
//   - timeout: время ожидания graceful shutdown
//   - > 0: при остановке клиента будет вызван OnStop для соединения и ожидание указанное время
//   - == 0: немедленное закрытие соединения без вызова OnStop
//
// Метод может быть вызван в любое время, в том числе до запуска клиента.
//
// Пример:
//
//	client.SetGracefulTimeout(5 * time.Second)
func (c *Client[T]) SetGracefulTimeout(timeout time.Duration) {
	c.gracefulTimeout = timeout
}

// Start запускает TCP клиент и начинает процесс подключения.
//
// Параметры:
//   - ctx: контекст для управления жизненным циклом клиента (при завершении клиент автоматически останавливается)
//   - parser: парсер протокола для чтения и записи пакетов
//   - handlers: обработчики событий клиента
//
// Возвращает:
//   - <-chan struct{}: канал, который закрывается при полной остановке клиента
//   - error: ошибка запуска или nil при успехе
//
// Метод создает соединение с сервером и запускает обработку событий.
// Если ReconnectEnabled = true, клиент будет автоматически переподключаться
// при разрыве соединения.
//
// При завершении переданного контекста, клиент автоматически вызывает Stop()
// с использованием graceful shutdown timeout (установленного через SetGracefulTimeout).
//
// Пример:
//
//	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
//	    OnConnected: func(conn *Connection[[]byte]) {
//	        log.Println("Connected!")
//	    },
//	    OnRead: func(ctx context.Context, conn *Connection[[]byte], data []byte) {
//	        log.Printf("Received: %s", data)
//	    },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	<-done // ждем полной остановки клиента
func (c *Client[T]) Start(
	ctx context.Context,
	parser ProtocolParser[T],
	handlers ClientHandlers[T],
) (<-chan struct{}, error) {
	if c.running.Load() {
		return nil, ErrClientAlreadyStarted
	}

	c.startOnce.Do(func() {
		c.parser = parser
		c.handlers = handlers

		// Создаем дочерний контекст
		c.ctx, c.cancel = context.WithCancel(ctx)

		// Инициализируем канал done
		c.done = make(chan struct{})

		c.running.Store(true)

		c.logger.Info("TCP client starting for %s", c.address)

		// Запускаем горутину управления жизненным циклом
		c.mainWg.Add(1)
		go c.connectionLoop()
	})

	return c.done, nil
}

// connect выполняет подключение к серверу (внутренний метод).
func (c *Client[T]) connect() error {
	// Проверяем контекст перед началом подключения
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	default:
	}

	c.logger.Info("Connecting to %s...", c.address)

	// Создаем контекст с таймаутом для подключения
	connectCtx, connectCancel := context.WithTimeout(c.ctx, c.config.ConnectTimeout)
	defer connectCancel()

	// Устанавливаем соединение
	var d net.Dialer
	conn, err := d.DialContext(connectCtx, "tcp", c.address)
	if err != nil {
		c.logger.Error("Failed to connect to %s: %v", c.address, err)
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.logger.Info("Connected to %s", c.address)

	// Создаем адаптер обработчиков
	connectionHandlers := c.createConnectionHandlers()

	// Увеличиваем WaitGroup для отслеживания соединения
	c.connWg.Add(1)

	// Создаем cleanup функцию, которая будет вызвана когда eventLoop завершится
	cleanupFunc := func() {
		// Уведомляем WaitGroup о завершении соединения
		c.connWg.Done()

		c.logger.Info("Connection #%d to %s cleaned up", c.conn.id, c.address)
	}

	// Создаем Connection с cleanupFunc
	c.mu.Lock()
	c.conn = newConnection(conn, c.parser, connectionHandlers, c.logger, c.ctx, cleanupFunc)
	c.mu.Unlock()

	c.connected.Store(true)

	// OnConnected теперь вызывается через OnConnect в eventLoop

	return nil
}

// createConnectionHandlers создает адаптер ConnectionHandlers из ClientHandlers.
func (c *Client[T]) createConnectionHandlers() ConnectionHandlers[T] {
	handlers := ConnectionHandlers[T]{
		OnConnected: func(ctx context.Context, conn *Connection[T]) {
			if c.handlers.OnConnected != nil {
				c.handlers.OnConnected(conn)
			}
		},
		OnRead: func(ctx context.Context, conn *Connection[T], packet T) {
			if c.handlers.OnRead != nil {
				c.handlers.OnRead(ctx, conn, packet)
			}
		},
		OnError: func(conn *Connection[T], err error) {
			if c.handlers.OnError != nil {
				c.handlers.OnError(conn, err)
			}
		},
		OnClosed: func(conn *Connection[T]) {
			wasConnected := c.connected.Swap(false)

			// Получаем причину закрытия
			var err error
			isDisconnecting := c.disconnecting.Load()
			if !isDisconnecting {
				err = errors.New("connection closed unexpectedly")
			}

			// Вызываем OnDisconnected всегда, если было подключение
			if c.handlers.OnDisconnected != nil && wasConnected {
				c.handlers.OnDisconnected(conn, err)
			}

			// Запускаем переподключение, если не в процессе отключения
			if !isDisconnecting && c.config.ReconnectEnabled {
				select {
				case c.reconnectCh <- struct{}{}:
				default:
					// Канал уже заполнен, переподключение уже запланировано
				}
			}
		},
	}

	// Устанавливаем OnStop только если он задан пользователем
	// Это важно для корректной работы автоматического закрытия при graceful shutdown
	if c.handlers.OnStop != nil {
		handlers.OnStop = func(conn *Connection[T]) {
			c.handlers.OnStop(conn)
		}
	}

	return handlers
}

// connectionLoop - главная горутина управления жизненным циклом клиента.
// Выполняет подключение, переподключение и мониторинг контекста.
func (c *Client[T]) connectionLoop() {
	defer c.mainWg.Done()

	// Выполняем первое подключение
	if err := c.connect(); err != nil {
		c.logger.Error("Failed to establish initial connection: %v", err)

		// Вызываем OnError для первой неудачной попытки
		// conn передаем nil т.к. соединение не установлено
		if c.handlers.OnError != nil {
			c.handlers.OnError(nil, ErrConnectionFailed)
		}

		// Если переподключение не включено, выходим
		if !c.config.ReconnectEnabled {
			return
		}
		// Иначе запускаем процесс переподключения
		select {
		case c.reconnectCh <- struct{}{}:
		default:
		}
	}

	var (
		attempt        = 0
		reconnectTimer <-chan time.Time // nil канал пока не нужен таймаут
		needReconnect  = false
	)

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Context cancelled, stopping client...")
			return

		case <-c.reconnectCh:
			// Проверяем что переподключение включено
			if !c.config.ReconnectEnabled {
				continue
			}

			// Сбрасываем счетчик попыток при успешном переподключении после разрыва
			if c.connected.Load() {
				attempt = 0
				reconnectTimer = nil
				needReconnect = false
				continue
			}

			// Начинаем процесс переподключения
			needReconnect = true
			attempt++

			// Проверяем лимит попыток
			if c.config.MaxReconnectAttempts > 0 && attempt > c.config.MaxReconnectAttempts {
				c.logger.Error("Max reconnect attempts (%d) reached", c.config.MaxReconnectAttempts)
				// conn передаем nil т.к. соединение не восстановлено
				if c.handlers.OnError != nil {
					c.handlers.OnError(nil, ErrReconnectFailed)
				}
				return
			}

			// Вычисляем задержку с экспоненциальным backoff
			delay := c.calculateReconnectDelay(attempt)

			c.logger.Info("Reconnecting to %s (attempt %d) after %v...", c.address, attempt, delay)

			// Устанавливаем таймер для переподключения (канал больше не nil)
			reconnectTimer = time.After(delay)

		case <-reconnectTimer:
			// Таймер сработал, пора переподключаться
			reconnectTimer = nil // Сбрасываем канал в nil

			if !needReconnect {
				continue
			}

			// Вызываем OnReconnecting перед попыткой
			if c.handlers.OnReconnecting != nil {
				c.handlers.OnReconnecting(attempt)
			}

			// Пытаемся подключиться
			if err := c.connect(); err != nil {
				c.logger.Error("Reconnect attempt %d failed: %v", attempt, err)

				// Вызываем OnError для каждой неудачной попытки
				if c.handlers.OnError != nil {
					c.mu.RLock()
					conn := c.conn
					c.mu.RUnlock()
					c.handlers.OnError(conn, ErrConnectionFailed)
				}

				// Планируем следующую попытку
				select {
				case c.reconnectCh <- struct{}{}:
				default:
				}
				continue
			}

			// Успешное переподключение
			c.logger.Info("Reconnected successfully after %d attempts", attempt)
			attempt = 0
			needReconnect = false
		}
	}
}

// calculateReconnectDelay вычисляет задержку для попытки переподключения.
func (c *Client[T]) calculateReconnectDelay(attempt int) time.Duration {
	// Экспоненциальный backoff: baseDelay * 2^(attempt-1)
	delay := float64(c.config.ReconnectBaseDelay) * math.Pow(2, float64(attempt-1))

	// Ограничиваем максимальной задержкой
	if delay > float64(c.config.ReconnectMaxDelay) {
		delay = float64(c.config.ReconnectMaxDelay)
	}

	return time.Duration(delay)
}

// Stop останавливает TCP клиент с graceful shutdown.
//
// Возвращает:
//   - error: ошибка остановки или nil при успехе
//
// Процесс остановки:
//  1. Отменяет контекст клиента (останавливает connectionLoop)
//  2. Если gracefulTimeout > 0: отправляет сигнал shutdown соединению (вызывает OnStop)
//  3. Ждет gracefulTimeout или пока соединение не закроется
//  4. Принудительно закрывает соединение если оно еще открыто
//  5. Для закрытого соединения вызывается OnClosed
//  6. Закрывает канал done для уведомления о полной остановке
//
// Таймаут graceful shutdown устанавливается через SetGracefulTimeout.
// Если таймаут равен 0, соединение закрывается немедленно без graceful shutdown.
//
// Метод потокобезопасен и может быть вызван многократно.
//
// Пример:
//
//	// Установить таймаут и остановить клиент
//	client.SetGracefulTimeout(5 * time.Second)
//	client.Stop()
func (c *Client[T]) Stop() error {
	if !c.running.Load() {
		return ErrClientNotStarted
	}

	var stopErr error
	c.stopOnce.Do(func() {
		c.logger.Info("Stopping TCP client...")

		c.disconnecting.Store(true)

		// Отменяем контекст клиента первым делом, чтобы connectionLoop корректно завершился
		if c.cancel != nil {
			c.cancel()
		}

		// Если gracefulTimeout > 0, выполняем graceful shutdown
		if c.gracefulTimeout > 0 {
			c.logger.Info("Starting graceful shutdown with timeout %v", c.gracefulTimeout)

			// Получаем текущее соединение
			c.mu.RLock()
			conn := c.conn
			c.mu.RUnlock()

			// Уведомляем соединение о shutdown
			if conn != nil && !conn.IsClosed() {
				conn.NotifyShutdown()
			}

			// Ждем gracefulTimeout или пока соединение не закроется
			done := make(chan struct{})
			go func() {
				c.connWg.Wait()
				close(done)
			}()

			select {
			case <-done:
				c.logger.Info("Connection closed gracefully")
			case <-time.After(c.gracefulTimeout):
				c.logger.Warn("Graceful shutdown timeout, forcefully closing connection")
			}
		}

		// Принудительно закрываем соединение если еще открыто
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn != nil && !conn.IsClosed() {
			if err := conn.Close(true); err != nil {
				c.logger.Error("Error closing connection: %v", err)
				stopErr = err
			}
		}

		// Ждем завершения connectionLoop
		c.mainWg.Wait()

		// Ждем завершения всех соединений (важно даже если gracefulTimeout == 0)
		c.connWg.Wait()

		c.running.Store(false)
		c.logger.Info("TCP client stopped")
		c.connected.Store(false)
		c.disconnecting.Store(false)
		// Закрываем канал done для уведомления о полной остановке
		if c.done != nil {
			close(c.done)
		}

	})

	return stopErr
}

// IsConnected возвращает true, если клиент подключен к серверу.
func (c *Client[T]) IsConnected() bool {
	return c.connected.Load()
}

// GetConnection возвращает текущее соединение.
// Возвращает nil, если клиент не подключен.
//
// Примечание: соединение может быть закрыто в любой момент,
// поэтому всегда проверяйте результат на nil.
func (c *Client[T]) GetConnection() *Connection[T] {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

// Write отправляет пакет данных на сервер.
// Это удобный метод, который использует текущее соединение.
//
// Параметры:
//   - ctx: контекст для управления отменой операции
//   - packet: пакет данных для отправки
//
// Возвращает:
//   - error: ошибка отправки или nil при успехе
//
// Если клиент не подключен, возвращает ErrClientNotConnected.
func (c *Client[T]) Write(ctx context.Context, packet T) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil || !c.connected.Load() {
		return ErrClientNotConnected
	}

	return conn.Write(ctx, packet)
}

// GetAddress возвращает адрес сервера.
func (c *Client[T]) GetAddress() string {
	return c.address
}
