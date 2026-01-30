package rltcpkit

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)


// connectionIDCounter - глобальный счетчик для генерации уникальных ID соединений
var connectionIDCounter atomic.Uint64

// Connection представляет обёртку вокруг TCP соединения с поддержкой каналов
// для чтения, записи и обработки ошибок.
//
// Параметр типа T должен соответствовать типу данных, используемому в ProtocolParser.
//
// Connection автоматически управляет горутинами для чтения и записи данных,
// обеспечивая асинхронную работу с соединением через каналы.
type Connection[T any] struct {
	// id - уникальный идентификатор соединения
	id uint64

	// conn - базовое TCP соединение
	conn net.Conn

	// parser - парсер протокола для чтения и записи пакетов
	parser ProtocolParser[T]

	// Каналы для асинхронной работы
	writeChan chan T
	readChan  chan T
	errorChan chan error

	// handlers - текущие обработчики событий (хранится через atomic.Value)
	handlers atomic.Value // ConnectionHandlers[T]

	// userData - пользовательские данные, связанные с соединением (хранится через atomic.Value)
	// Может использоваться для хранения состояния сессии, информации о пользователе и т.д.
	userData atomic.Value // interface{}

	// Управление жизненным циклом
	ctx           context.Context
	cancel        context.CancelFunc
	closed        atomic.Bool
	closeOnce     sync.Once
	readCloseOnce sync.Once
	shutdownCh    chan struct{} // канал для сигнала graceful shutdown

	// cleanupFunc вызывается когда соединение завершается (из eventLoop)
	cleanupFunc func()

	// logger для логгирования событий соединения
	logger Logger
}

// newConnection создает новое соединение с указанными параметрами.
// Это внутренняя функция, используемая сервером при принятии нового соединения.
func newConnection[T any](
	conn net.Conn,
	parser ProtocolParser[T],
	handlers ConnectionHandlers[T],
	logger Logger,
	parentCtx context.Context,
	cleanupFunc func(),
) *Connection[T] {
	ctx, cancel := context.WithCancel(parentCtx)

	c := &Connection[T]{
		id:          connectionIDCounter.Add(1),
		conn:        conn,
		parser:      parser,
		writeChan:   make(chan T, 100), // буферизованный канал для записи
		readChan:    make(chan T, 100), // буферизованный канал для чтения
		errorChan:   make(chan error, 10),
		ctx:         ctx,
		cancel:      cancel,
		shutdownCh:  make(chan struct{}),
		logger:      logger,
		cleanupFunc: cleanupFunc,
	}

	// Инициализируем atomic.Value для handlers и userData
	c.handlers.Store(handlers)

	// Запускаем только главный eventLoop, который управляет остальными горутинами
	go c.eventLoop()

	return c
}

// readGoroutine выполняет чтение данных из соединения в отдельной горутине.
// Отправляет прочитанные пакеты в readChan, ошибки в errorChan.
// Завершается при закрытии сокета (ReadPacket вернет ошибку).
func (c *Connection[T]) readGoroutine() {
	defer func() {
		c.readCloseOnce.Do(func() {
			close(c.readChan)
		})
		c.logger.Info("Connection #%d read loop closed %s", c.id, c.RemoteAddr())
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		packet, err := c.parser.ReadPacket(c.ctx, c.conn)
		if err != nil {
			// EOF - нормальное завершение соединения, закрываемся без ошибок
			if errors.Is(err, io.EOF) {
				c.logger.Info("Socket of connection #%d closed by remote peer (EOF)", c.id)
				return
			}
			// Проверяем, является ли ошибка таймаутом
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				// Если контекст отменен, таймаут ожидаем - не сообщаем об ошибке
				select {
				case <-c.ctx.Done():
					return
				default:
					// Контекст не отменен, таймаут неожиданный - сообщаем об ошибке
				}
			}

			// Отправляем ошибку в канал ошибок
			c.logger.Info("Connection #%d sending error to errorChan: %v", c.id, err)
			c.errorChan <- err
			return
		}

		// Пропускаем nil пакеты
		if any(packet) == nil {
			continue
		}

		// Отправляем прочитанный пакет в канал чтения (без вызова обработчика)
		c.readChan <- packet

	}
}

// writeGoroutine выполняет запись данных в соединение в отдельной горутине.
// Завершается только когда канал writeChan закрывается или происходит ошибка записи.
// Это позволяет завершить запись всех буферизованных данных при graceful shutdown.
func (c *Connection[T]) writeGoroutine() {
	defer func() {
		c.logger.Info("Connection #%d write loop closed %s", c.id, c.RemoteAddr())
	}()

	for packet := range c.writeChan {
		err := c.parser.WritePacket(c.conn, packet)
		if err != nil {
			// Проверяем, не закрыли ли мы сами соединение через Close()
			if errors.Is(err, net.ErrClosed) {
				// Соединение закрыто через Close() - нормальное завершение
				return
			}

			// Отправляем ошибку в канал ошибок, если возможно
			select {
			case c.errorChan <- err:
			default:
				// Канал ошибок заполнен или закрыт, просто выходим
			}
			return
		}
	}
}

// eventLoop - главная горутина обработки событий соединения.
// Читает из readChan, errorChan, shutdownCh и вызывает соответствующие обработчики.
// Завершается только при явном закрытии или ошибке, НЕ при ctx.Done() (для graceful shutdown).
func (c *Connection[T]) eventLoop() {
	defer c.logger.Info("Connection #%d event loop finished %s", c.id, c.RemoteAddr())
	// Внутренний WaitGroup для read/write горутин
	var ioWg sync.WaitGroup

	// Запускаем горутины чтения и записи
	ioWg.Add(2)
	go func() {
		defer ioWg.Done()
		c.readGoroutine()
	}()
	go func() {
		defer ioWg.Done()
		c.writeGoroutine()
	}()

	defer func() {

		// Отменяем контекст что бы завершилась readGorutine (Конекст уже может быть отменён)
		c.cancel()

		// Закрываем writeChan, чтобы writeGoroutine завершилась
		c.closeOnce.Do(func() {
			close(c.writeChan)
		})

		// Ждем завершения read/write горутин
		ioWg.Wait()

		// Закрываем базовое соединение
		c.conn.Close()

		// Закрываем каналы
		close(c.errorChan)

		// Вызываем OnClosed callback
		handlers := c.handlers.Load().(ConnectionHandlers[T])
		if handlers.OnClosed != nil {
			handlers.OnClosed(c)
		}

		// Cleanup функция вызывается последней
		if c.cleanupFunc != nil {
			c.cleanupFunc()
		}

		// Устанавливаем флаг closed в самом конце, когда всё действительно завершено
		c.closed.Store(true)

		c.logger.Info("Connection #%d event loop closed %s", c.id, c.RemoteAddr())
	}()

	// Вызываем обработчик OnConnected после регистрации defer и запуска горутин
	handlers := c.handlers.Load().(ConnectionHandlers[T])
	if handlers.OnConnected != nil {
		handlers.OnConnected(c.ctx, c)
	}

	shutdownCh := c.shutdownCh // локальная копия для предотвращения busy loop

	for {
		// Проверяем контекст в начале каждой итерации
		if c.ctx.Err() != nil {
			c.logger.Info("Connection #%d eventLoop: context cancelled, exiting", c.id)
			return
		}

		c.logger.Info("Connection #%d eventLoop: waiting for events (shutdownCh=%v, errorChan buffer=%d/%d, readChan buffer=%d/%d)",
			c.id, shutdownCh != nil, len(c.errorChan), cap(c.errorChan), len(c.readChan), cap(c.readChan))

		select {
		case packet, ok := <-c.readChan:
			c.logger.Info("Connection #%d eventLoop: received packet from readChan (ok=%v)", c.id, ok)
			if !ok {
				// Канал закрыт, выходим
				if c.ctx.Err() == nil {
					c.logger.Error("Connection #%d eventLoop: readChan closed unexpectedly (context not done)", c.id)
				}
				c.logger.Info("Connection #%d eventLoop: readChan closed, exiting", c.id)
				return
			}
			// Вызываем обработчик OnRead
			c.logger.Info("Connection #%d eventLoop: calling OnRead handler", c.id)
			handlers := c.handlers.Load().(ConnectionHandlers[T])
			if handlers.OnRead != nil {
				handlers.OnRead(c.ctx, c, packet)
			}
			c.logger.Info("Connection #%d eventLoop: OnRead handler completed", c.id)

		case err, ok := <-c.errorChan:
			c.logger.Info("Connection #%d eventLoop: received error from errorChan (ok=%v, err=%v)", c.id, ok, err)
			if !ok {
				// Канал закрыт, выходим
				c.logger.Info("Connection #%d eventLoop: errorChan closed, exiting", c.id)
				return
			}
			c.logger.Info("Connection #%d received error from errorChan: type=%T, err=%v", c.id, err, err)

			c.logger.Error("Connection #%d error: %v", c.id, err)
			// Вызываем обработчик OnError
			handlers := c.handlers.Load().(ConnectionHandlers[T])
			if handlers.OnError != nil {
				handlers.OnError(c, err)
			}

			// После ошибки закрываем соединение и выходим
			c.Close(true)
			return

		case <-shutdownCh:
			c.logger.Info("Connection #%d eventLoop: received shutdown signal", c.id)
			// Вызываем обработчик OnStop синхронно
			handlers := c.handlers.Load().(ConnectionHandlers[T])
			if handlers.OnStop != nil {
				c.logger.Info("Connection #%d eventLoop: calling OnStop handler", c.id)
				handlers.OnStop(c)
				c.logger.Info("Connection #%d eventLoop: OnStop handler completed", c.id)
			} else {
				// Если обработчик OnStop не установлен, сразу закрываем соединение
				c.logger.Info("Connection #%d eventLoop: no OnStop handler, closing connection", c.id)
				c.Close(false) // мягкое закрытие для завершения отправки буферизованных данных
				return
			}
			// Устанавливаем канал в nil, чтобы этот case больше не срабатывал
			c.logger.Info("Connection #%d eventLoop: setting shutdownCh to nil", c.id)
			shutdownCh = nil
			//Временно закоментировано
			// case <-c.ctx.Done():
			// 	c.logger.Info("Connection #%d eventLoop: context done (closed=%v)", c.id, c.closed.Load())
			// 	// Контекст отменён - проверяем, закрыто ли соединение
			// 	if c.closed.Load() {
			// 		c.logger.Info("Connection #%d eventLoop: connection closed after ctx.Done(), exiting", c.id)
			// 		return
			// 	}
			// 	c.logger.Info("Connection #%d eventLoop: connection not closed after ctx.Done(), continuing for graceful shutdown", c.id)
			// 	// Если не закрыто, продолжаем работать для graceful shutdown
		}
	}
}

// Write отправляет пакет данных в канал записи для асинхронной отправки.
// Метод не блокируется, если в канале есть место.
//
// Принимает контекст для управления отменой операции записи.
// Возвращает ошибку, если соединение уже закрыто или контекст отменён.
func (c *Connection[T]) Write(ctx context.Context, packet T) error {

	select {
	case c.writeChan <- packet:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetHandlers заменяет текущие обработчики событий новыми.
// Этот метод потокобезопасен и может быть вызван во время работы соединения.
//
// Полезно для изменения поведения после определенных событий,
// например, после успешной аутентификации.
func (c *Connection[T]) SetHandlers(handlers ConnectionHandlers[T]) {
	c.handlers.Store(handlers)
}

// SetUserData устанавливает пользовательские данные для соединения.
// Этот метод потокобезопасен.
func (c *Connection[T]) SetUserData(data interface{}) {
	c.userData.Store(data)
}

// GetUserData получает пользовательские данные соединения.
// Возвращает nil, если данные не были установлены.
func (c *Connection[T]) GetUserData() interface{} {
	return c.userData.Load()
}

// GetID возвращает уникальный идентификатор соединения.
// ID генерируется автоматически при создании соединения и остается
// неизменным на протяжении всего жизненного цикла соединения.
// Может использоваться для логгирования и отладки.
func (c *Connection[T]) GetID() uint64 {
	return c.id
}

// RemoteAddr возвращает удаленный адрес соединения.
func (c *Connection[T]) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// RawConn exposes the underlying net.Conn for protocol handshakes that bypass the parser.
func (c *Connection[T]) RawConn() net.Conn {
	return c.conn
}

// LocalAddr возвращает локальный адрес соединения.
func (c *Connection[T]) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// NotifyShutdown уведомляет соединение о начале graceful shutdown.
// Обработчик OnStop будет вызван синхронно из eventLoop.
func (c *Connection[T]) NotifyShutdown() {
	select {
	case <-c.shutdownCh:
		// Уже уведомлены
		return
	default:
		close(c.shutdownCh)
	}
}

// IsShuttingDown возвращает true, если соединение находится в процессе graceful shutdown.
func (c *Connection[T]) IsShuttingDown() bool {
	select {
	case <-c.shutdownCh:
		return true
	default:
		return false
	}
}

// Close закрывает соединение и освобождает все связанные ресурсы.
// Метод может быть вызван многократно безопасно (идемпотентен).
//
// Параметры:
//   - force: если true, немедленно закрывает сокет, прерывая все операции чтения/записи;
//     если false, устанавливает deadline в 0, что будит операции чтения и позволяет
//     завершиться текущим операциям записи или дождаться таймаута write deadline
//
// После закрытия вызывается обработчик OnClosed, если он установлен.
// Повторные вызовы с другим значением force выполнят соответствующую операцию закрытия.
func (c *Connection[T]) Close(force bool) error {
	// Отменяем контекст (потокобезопасно, идемпотентно)
	// Это сигнализирует о начале процесса закрытия
	c.cancel()

	// Закрываем канал записи, чтобы writeGoroutine завершилась
	// после обработки всех буферизованных данных
	// (sync.Once гарантирует выполнение только один раз)
	c.closeOnce.Do(func() {
		close(c.writeChan)
	})

	// Выполняем операцию закрытия в соответствии с параметром force
	// Эти операции потокобезопасны и могут быть вызваны повторно
	var err error
	if force {
		// Жесткое закрытие - немедленно закрываем сокет. Прервутся операции чтения и записи.
		err = c.conn.Close()
	} else {
		// Мягкое закрытие - устанавливаем deadline в 0, чтобы разбудить операции чтения для завершения readGorutine.
		// writeGorutine завершится после отправки всех данных.
		err = c.conn.SetReadDeadline(time.Now())
	}

	return err
}

// IsClosed возвращает true, если соединение закрыто.
func (c *Connection[T]) IsClosed() bool {
	return c.closed.Load()
}

// SetReadDeadline устанавливает абсолютное время deadline для операций чтения.
// Значение t равное нулю означает, что операции чтения не будут иметь таймаута.
//
// После установки deadline, все текущие и будущие операции чтения будут
// возвращать ошибку при превышении времени.
func (c *Connection[T]) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline устанавливает абсолютное время deadline для операций записи.
// Значение t равное нулю означает, что операции записи не будут иметь таймаута.
//
// После установки deadline, все текущие и будущие операции записи будут
// возвращать ошибку при превышении времени.
func (c *Connection[T]) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// SetDeadline устанавливает абсолютное время deadline для операций чтения и записи.
// Эквивалентно вызову SetReadDeadline и SetWriteDeadline.
// Значение t равное нулю означает, что операции не будут иметь таймаута.
func (c *Connection[T]) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

// SetReadTimeout устанавливает относительный таймаут для операций чтения.
// Таймаут применяется к каждой операции чтения отдельно.
// Значение 0 означает отсутствие таймаута.
//
// Это удобный метод, который вызывает SetReadDeadline(time.Now().Add(timeout)).
func (c *Connection[T]) SetReadTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return c.conn.SetReadDeadline(time.Time{})
	}
	return c.conn.SetReadDeadline(time.Now().Add(timeout))
}

// SetWriteTimeout устанавливает относительный таймаут для операций записи.
// Таймаут применяется к каждой операции записи отдельно.
// Значение 0 означает отсутствие таймаута.
//
// Это удобный метод, который вызывает SetWriteDeadline(time.Now().Add(timeout)).
func (c *Connection[T]) SetWriteTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return c.conn.SetWriteDeadline(time.Time{})
	}
	return c.conn.SetWriteDeadline(time.Now().Add(timeout))
}

// SetTimeout устанавливает относительный таймаут для операций чтения и записи.
// Эквивалентно вызову SetReadTimeout и SetWriteTimeout.
// Значение 0 означает отсутствие таймаута.
func (c *Connection[T]) SetTimeout(timeout time.Duration) error {
	if err := c.SetReadTimeout(timeout); err != nil {
		return err
	}
	return c.SetWriteTimeout(timeout)
}
