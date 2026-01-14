// Package tcpserver предоставляет универсальный, переиспользуемый TCP сервер с поддержкой generic протоколов.
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
// Connection - обёртка вокруг net.Conn с каналами для асинхронной работы
// ProtocolParser - интерфейс для парсинга протоколов
// ConnectionHandlers - набор обработчиков событий соединения
// Logger - интерфейс для логгирования
//
// Пример использования:
//
//	// Создаем сервер
//	server := tcpserver.NewServer[[]byte](":8080", tcpserver.Config{
//	    MaxConnections: 1000,
//	    Logger: myLogger,
//	})
//
//	// Устанавливаем graceful shutdown timeout
//	server.SetGracefulTimeout(5 * time.Second)
//
//	// Создаем парсер для байтовых данных
//	parser := tcpserver.NewByteParser()
//
//	// Запускаем сервер
//	done, err := server.Start(context.Background(), parser, func(conn *tcpserver.Connection[[]byte]) tcpserver.ConnectionHandlers[[]byte] {
//	    return tcpserver.ConnectionHandlers[[]byte]{
//	        OnRead: func(ctx context.Context, c *tcpserver.Connection[[]byte], data []byte) {
//	            c.Write(data) // echo
//	        },
//	        OnError: func(c *tcpserver.Connection[[]byte], err error) {
//	            log.Printf("Error: %v", err)
//	        },
//	        OnClosed: func(c *tcpserver.Connection[[]byte]) {
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
package tcpserver
