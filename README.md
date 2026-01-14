# TCP Server - Универсальный пакет для TCP серверов на Go

Гибкий, переиспользуемый пакет для создания TCP серверов с поддержкой generic протоколов, асинхронной работы через каналы и graceful shutdown.

## Возможности

- ✅ **Generic протоколы** - используйте любой тип данных через `ProtocolParser[T]`
- ✅ **Асинхронная работа** - каналы для чтения, записи и обработки ошибок
- ✅ **Гибкие обработчики** - `OnRead`, `OnError`, `OnStop`, `OnClosed`
- ✅ **Graceful shutdown** - корректная остановка с настраиваемым таймаутом
- ✅ **Ограничение подключений** - контроль максимального количества соединений
- ✅ **Динамическая смена обработчиков** - например, после авторизации
- ✅ **Пользовательские данные** - привязка данных к каждому соединению
- ✅ **Гибкое логгирование** - собственный интерфейс логгера
- ✅ **Потокобезопасность** - готов к использованию в многопоточной среде

## Установка

```bash
go get github.com/example/tcpserver
```

## Быстрый старт

### Простой Echo сервер

```go
package main

import (
    "context"
    "log"
    "github.com/example/tcpserver/pkg/tcpserver"
)

func main() {
    // Создаем сервер
    server := tcpserver.NewServer[[]byte](":8080", tcpserver.Config{
        MaxConnections: 100,
        Logger:         tcpserver.NewNoopLogger(),
    })

    // Создаем парсер для байтовых данных
    parser := tcpserver.NewByteParser()

    // Запускаем сервер
    err := server.Start(context.Background(), parser, 
        func(conn *tcpserver.Connection[[]byte]) tcpserver.ConnectionHandlers[[]byte] {
            return tcpserver.ConnectionHandlers[[]byte]{
                OnRead: func(c *tcpserver.Connection[[]byte], data []byte) {
                    c.Write(data) // Echo обратно
                },
                OnError: func(c *tcpserver.Connection[[]byte], err error) {
                    log.Printf("Error: %v", err)
                },
            }
        })

    if err != nil {
        log.Fatal(err)
    }

    // Graceful shutdown через 5 секунд
    server.Stop(5 * time.Second)
}
```

### Запуск примера

```bash
# Сборка и запуск echo сервера
cd example
go run main.go -addr :8080 -max-conn 100

# В другом терминале
telnet localhost 8080
```

## Архитектура

### Основные компоненты

#### 1. Server[T]
Главный компонент, управляющий TCP сервером:
- Принимает входящие подключения
- Управляет жизненным циклом соединений
- Поддерживает graceful shutdown
- Контролирует максимальное количество подключений

#### 2. Connection[T]
Обёртка вокруг `net.Conn` с расширенными возможностями:
- Три канала: для чтения, записи и ошибок
- Горутины для асинхронного чтения/записи
- Пользовательские данные (`UserData`)
- Динамическая смена обработчиков

#### 3. ProtocolParser[T]
Интерфейс для определения протокола передачи данных:
```go
type ProtocolParser[T any] interface {
    ReadPacket(ctx context.Context, conn net.Conn) (T, error)
    WritePacket(conn net.Conn, packet T) error
}
```

Встроенная реализация: `ByteParser` для работы с `[]byte`.

#### 4. ConnectionHandlers[T]
Набор callback-функций для обработки событий:
- `OnRead` - получение данных
- `OnError` - обработка ошибок
- `OnStop` - graceful shutdown
- `OnClosed` - закрытие соединения

## Продвинутое использование

### Кастомный протокол

```go
type Message struct {
    Type string
    Data []byte
}

type MyProtocol struct{}

func (p *MyProtocol) ReadPacket(ctx context.Context, conn net.Conn) (Message, error) {
    // Читаем длину сообщения (4 байта)
    var length uint32
    binary.Read(conn, binary.BigEndian, &length)
    
    // Читаем тело сообщения
    data := make([]byte, length)
    io.ReadFull(conn, data)
    
    return Message{Data: data}, nil
}

func (p *MyProtocol) WritePacket(conn net.Conn, msg Message) error {
    // Пишем длину
    binary.Write(conn, binary.BigEndian, uint32(len(msg.Data)))
    
    // Пишем данные
    _, err := conn.Write(msg.Data)
    return err
}

// Использование
server := tcpserver.NewServer[Message](":8080", config)
parser := &MyProtocol{}
server.Start(ctx, parser, onAcceptHandler)
```

### Смена обработчиков после авторизации

```go
server.Start(ctx, parser, func(conn *Connection[Message]) ConnectionHandlers[Message] {
    return ConnectionHandlers[Message]{
        OnRead: func(c *Connection[Message], msg Message) {
            // Проверяем авторизацию
            if c.GetUserData() == nil {
                if msg.Type == "AUTH" && validateAuth(msg.Data) {
                    c.SetUserData("authorized")
                    c.SetHandlers(getAuthorizedHandlers()) // Меняем обработчики
                    c.Write(Message{Type: "AUTH_OK"})
                } else {
                    c.Write(Message{Type: "AUTH_REQUIRED"})
                }
            }
        },
    }
})
```

### Пользовательские данные

```go
type UserSession struct {
    Username string
    LoginTime time.Time
    IsAdmin bool
}

OnRead: func(c *Connection[Message], msg Message) {
    session := c.GetUserData().(*UserSession)
    if session.IsAdmin {
        // Админские права
    }
}
```

### Кастомный логгер

```go
type MyLogger struct {
    logger *slog.Logger
}

func (l *MyLogger) Info(msg string, args ...interface{}) {
    l.logger.Info(fmt.Sprintf(msg, args...))
}

func (l *MyLogger) Warn(msg string, args ...interface{}) {
    l.logger.Warn(fmt.Sprintf(msg, args...))
}

func (l *MyLogger) Error(msg string, args ...interface{}) {
    l.logger.Error(fmt.Sprintf(msg, args...))
}
```

## Конфигурация

```go
type Config struct {
    // Максимальное количество подключений (0 = без ограничений)
    MaxConnections int
    
    // Логгер (nil = NoopLogger)
    Logger Logger
    
    // Размер буфера чтения (0 = по умолчанию)
    ReadBufferSize int
    
    // Размер буфера записи (0 = по умолчанию)
    WriteBufferSize int
}
```

## Graceful Shutdown

```go
// С таймаутом - вызывает OnStop для всех соединений
server.Stop(5 * time.Second)

// Немедленная остановка - без вызова OnStop
server.Stop(0)
```

**Процесс graceful shutdown:**
1. Закрывается listener (новые подключения не принимаются)
2. Если timeout > 0, вызывается `OnStop` для всех соединений
3. Ожидание timeout или закрытия всех соединений
4. Принудительное закрытие оставшихся соединений
5. Вызов `OnClosed` для каждого соединения

## API Reference

### Server[T]

```go
// Создание сервера
NewServer[T](address string, config Config) *Server[T]

// Запуск сервера
Start(ctx context.Context, parser ProtocolParser[T], 
      onAccept func(*Connection[T]) ConnectionHandlers[T]) error

// Остановка сервера
Stop(timeout time.Duration) error

// Получение количества подключений
GetConnectionCount() int64

// Проверка статуса
IsRunning() bool

// Получение адреса
GetAddress() string

// Итерация по всем соединениям
ForEachConnection(fn func(*Connection[T]) bool)
```

### Connection[T]

```go
// Асинхронная отправка данных
Write(packet T) error

// Синхронное чтение данных
Read() (T, error)

// Получение ошибки
GetError() error

// Смена обработчиков
SetHandlers(handlers ConnectionHandlers[T])

// Пользовательские данные
SetUserData(data interface{})
GetUserData() interface{}

// Информация о соединении
RemoteAddr() net.Addr
LocalAddr() net.Addr
IsClosed() bool
IsShuttingDown() bool

// Закрытие соединения
Close() error
```

## Тестирование

```bash
# Запустить сервер
go run example/main.go -addr :8080

# В другом терминале - тест с telnet
telnet localhost 8080

# Или с netcat
echo "Hello, World!" | nc localhost 8080
```

## Производительность

- Горутины на каждое соединение для асинхронного I/O
- Буферизованные каналы для минимизации блокировок
- `sync.Map` для эффективного управления соединениями
- Атомарные операции для счетчиков
- Zero-copy где возможно

## Требования

- Go 1.25+ (для поддержки generics)

## Лицензия

MIT

## Автор

Создано для использования в различных проектах, требующих надежный TCP сервер.
