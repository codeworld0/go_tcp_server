// Package rltcpkit предоставляет универсальный, переиспользуемый TCP сервер с поддержкой generic протоколов.
//
// Основные возможности:
//   - Generic протоколы через интерфейс ProtocolParser[T]
//   - Гибкая система обработчиков событий (OnRead, OnError, OnStop, OnClosed)
//   - Асинхронная работа через каналы для чтения, записи и ошибок
//   - Graceful shutdown с настраиваемым таймаутом
//   - Ограничение максимального количества подключений
//   - Поддержка пользовательских данных для каждого соединения
//   - Возможность смены обработчиков во время работы (например, после авторизации)
//   - Гибкое логгирование через интерфейс Logger
//
// Основные компоненты:
//
// Server - основной TCP сервер, управляющий подключениями
// Client - TCP клиент с автоматическим переподключением
// Connection - обёртка вокруг net.Conn с каналами для асинхронной работы
// ProtocolParser - интерфейс для парсинга протоколов
// ConnectionHandlers - набор обработчиков событий соединения
// ClientHandlers - набор обработчиков событий клиента
// Logger - интерфейс для логгирования
//
// Пример использования:
//
//	// Создаем сервер
//	server := rltcpkit.NewServer[[]byte](":8080", rltcpkit.Config{
//	    MaxConnections: 1000,
//	    Logger: myLogger,
//	})
//
//	// Устанавливаем graceful shutdown timeout
//	server.SetGracefulTimeout(5 * time.Second)
//
//	// Создаем парсер для байтовых данных
//	parser := rltcpkit.NewByteParser()
//
//	// Запускаем сервер
//	done, err := server.Start(context.Background(), parser, func(conn *rltcpkit.Connection[[]byte]) rltcpkit.ConnectionHandlers[[]byte] {
//	    return rltcpkit.ConnectionHandlers[[]byte]{
//	        OnRead: func(ctx context.Context, c *rltcpkit.Connection[[]byte], data []byte) {
//	            c.Write(data) // echo
//	        },
//	        OnError: func(c *rltcpkit.Connection[[]byte], err error) {
//	            log.Printf("Error: %v", err)
//	        },
//	        OnClosed: func(c *rltcpkit.Connection[[]byte]) {
//	            log.Printf("Connection closed")
//	        },
//	    }
//	})
//
//	// Graceful shutdown
//	server.Stop()
//	<-done // Ждем полной остановки
//
// Создание кастомного протокола:
//
//	type MyProtocol struct{}
//
//	func (p *MyProtocol) ReadPacket(ctx context.Context, conn net.Conn) (MyMessage, error) {
//	    // Реализация чтения вашего протокола
//	}
//
//	func (p *MyProtocol) WritePacket(conn net.Conn, msg MyMessage) error {
//	    // Реализация записи вашего протокола
//	}
//
// Смена обработчиков после авторизации:
//
//	OnRead: func(ctx context.Context, c *Connection[MyMessage], msg MyMessage) {
//	    if !c.GetUserData().(bool) { // не авторизован
//	        if msg.Type == "AUTH" {
//	            c.SetUserData(true) // авторизован
//	            c.SetHandlers(authorizedHandlers) // меняем обработчики
//	        }
//	    } else {
//	        // обработка авторизованного пользователя
//	    }
//	}
//
// Использование клиента:
//
//	// Создаем клиент
//	client := rltcpkit.NewClient[[]byte]("localhost:8080", rltcpkit.ClientConfig{
//	    ConnectTimeout: 5 * time.Second,
//	    ReconnectEnabled: true,
//	    MaxReconnectAttempts: 5,
//	    ReconnectBaseDelay: 1 * time.Second,
//	    ReconnectMaxDelay: 30 * time.Second,
//	    Logger: myLogger,
//	})
//
//	// Создаем парсер для байтовых данных
//	parser := rltcpkit.NewByteParser()
//
//	// Подключаемся к серверу
//	err := client.Connect(context.Background(), parser, rltcpkit.ClientHandlers[[]byte]{
//	    OnConnected: func(conn *rltcpkit.Connection[[]byte]) {
//	        log.Println("Connected to server!")
//	    },
//	    OnDisconnected: func(conn *rltcpkit.Connection[[]byte], err error) {
//	        if err != nil {
//	            log.Printf("Disconnected: %v", err)
//	        }
//	    },
//	    OnRead: func(ctx context.Context, conn *rltcpkit.Connection[[]byte], data []byte) {
//	        log.Printf("Received: %s", data)
//	    },
//	    OnError: func(conn *rltcpkit.Connection[[]byte], err error) {
//	        log.Printf("Error: %v", err)
//	    },
//	    OnReconnecting: func(attempt int, nextDelay time.Duration) {
//	        log.Printf("Reconnecting (attempt %d) after %v...", attempt, nextDelay)
//	    },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Отправка данных
//	err = client.Write(context.Background(), []byte("Hello, server!"))
//	if err != nil {
//	    log.Printf("Write error: %v", err)
//	}
//
//	// Graceful disconnect
//	client.Disconnect()
//
// Автоматическое переподключение:
//
// Клиент поддерживает автоматическое переподключение с экспоненциальной задержкой.
// При разрыве соединения клиент автоматически пытается восстановить соединение
// согласно настройкам в ClientConfig:
//
//   - ReconnectEnabled: включает/выключает автоматическое переподключение
//   - MaxReconnectAttempts: максимальное количество попыток (0 = бесконечно)
//   - ReconnectBaseDelay: базовая задержка перед первой попыткой
//   - ReconnectMaxDelay: максимальная задержка между попытками
//
// Задержка вычисляется по формуле: min(baseDelay * 2^(attempt-1), maxDelay)
//
// Пример с автоматическим переподключением:
//
//	client := rltcpkit.NewClient[[]byte]("localhost:8080", rltcpkit.ClientConfig{
//	    ReconnectEnabled: true,
//	    MaxReconnectAttempts: 0, // бесконечные попытки
//	    ReconnectBaseDelay: 1 * time.Second,
//	    ReconnectMaxDelay: 60 * time.Second,
//	})
//
//	client.Connect(ctx, parser, rltcpkit.ClientHandlers[[]byte]{
//	    OnReconnecting: func(attempt int, nextDelay time.Duration) {
//	        log.Printf("Попытка переподключения #%d через %v", attempt, nextDelay)
//	    },
//	    OnConnected: func(conn *rltcpkit.Connection[[]byte]) {
//	        log.Println("Подключен! Отправляем данные...")
//	        // Переинициализация сессии после переподключения
//	    },
//	})
package rltcpkit
