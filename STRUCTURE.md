# Структура проекта TCP Server

```
.
├── go.mod                          # Go module файл
├── README.md                       # Основная документация проекта
├── STRUCTURE.md                    # Этот файл - описание структуры
├── Makefile                        # Makefile для сборки и тестирования
├── .gitignore                      # Git ignore файл
│
├── pkg/                            # Основной пакет библиотеки
│   └── tcpserver/                  # Пакет tcpserver
│       ├── doc.go                  # Документация пакета
│       ├── logger.go               # Интерфейс Logger и NoopLogger
│       ├── parser.go               # ProtocolParser интерфейс и ByteParser
│       ├── handlers.go             # ConnectionHandlers типы
│       ├── connection.go           # Connection обёртка с каналами
│       ├── server.go               # Server основной файл
│       └── server_test.go          # Тесты для сервера
│
└── example/                        # Примеры использования
    ├── README.md                   # Документация примеров
    ├── main.go                     # Echo сервер (основной пример)
    └── custom_protocol_example.go  # Пример с кастомным протоколом
```

## Описание файлов

### Корневая директория

- **go.mod** - определяет Go модуль и зависимости
- **README.md** - полная документация по использованию пакета
- **Makefile** - автоматизация сборки, тестирования и запуска
- **.gitignore** - исключает временные файлы из git

### pkg/tcpserver/

Основной пакет библиотеки, содержащий все компоненты TCP сервера:

#### doc.go
- Документация пакета в формате godoc
- Примеры использования
- Общее описание архитектуры

#### logger.go
- `Logger` interface - интерфейс логгирования
- `noopLogger` - реализация без действий
- Методы: Info, Warn, Error

#### parser.go
- `ProtocolParser[T]` interface - generic интерфейс для протоколов
- `ByteParser` struct - реализация для []byte
- Методы ReadPacket и WritePacket

#### handlers.go
- `ConnectionHandlers[T]` struct - набор callback функций
- OnRead, OnError, OnStop, OnClosed обработчики

#### connection.go (самый сложный файл)
- `Connection[T]` struct - обёртка вокруг net.Conn
- Три канала: writeChan, readChan, errorChan
- Три горутины: readLoop, writeLoop, errorLoop
- Методы для работы с соединением:
  - Write(packet T) - асинхронная запись
  - Read() (T, error) - синхронное чтение
  - GetError() error - получение ошибки
  - SetHandlers() - смена обработчиков
  - SetUserData/GetUserData - пользовательские данные
  - Close() - закрытие соединения

#### server.go
- `Server[T]` struct - основной TCP сервер
- `Config` struct - конфигурация сервера
- Методы:
  - NewServer[T]() - создание сервера
  - Start() - запуск сервера с accept loop
  - Stop() - graceful shutdown
  - GetConnectionCount() - текущее количество соединений
  - IsRunning() - статус сервера
  - ForEachConnection() - итерация по соединениям

#### server_test.go
- Unit тесты для всех компонентов
- Тесты:
  - TestServerStartStop - запуск/остановка
  - TestServerConnection - подключение
  - TestServerEcho - эхо функциональность
  - TestServerMaxConnections - лимит подключений
  - TestByteParser - работа парсера
  - TestConnectionUserData - пользовательские данные

### example/

Примеры использования пакета:

#### main.go
- Полноценный echo сервер
- Параметры командной строки
- Graceful shutdown по Ctrl+C
- Кастомный логгер
- Демонстрирует все обработчики событий

#### custom_protocol_example.go
- Пример с кастомным бинарным протоколом
- Структурированные сообщения (Message struct)
- Обработка команд (TIME, STATS, PING)
- Демонстрирует мощь generic подхода

#### README.md
- Документация по запуску примеров
- Параметры командной строки
- Примеры использования telnet/nc
- Пример вывода логов

## Ключевые особенности архитектуры

### 1. Generic подход
Все компоненты используют generics для поддержки любых типов данных:
- `Server[T]` - сервер для типа T
- `Connection[T]` - соединение с типом T
- `ProtocolParser[T]` - парсер для типа T

### 2. Асинхронная обработка
- Каналы для неблокирующей работы
- Горутины для каждого соединения (readLoop, writeLoop, errorLoop)

### 3. Гибкие обработчики
- Динамическая смена во время работы
- Опциональные (могут быть nil)
- Контекстно-зависимые (доступ к Connection)

### 4. Graceful shutdown
- Таймаут для корректного завершения
- Вызов OnStop перед закрытием
- Принудительное закрытие при timeout
- OnClosed для очистки ресурсов

### 5. Управление соединениями
- sync.Map для потокобезопасного хранения
- atomic счетчик для производительности
- Лимит максимальных подключений
- ForEachConnection для перебора

## Использование

### Сборка
```bash
make build          # Сборка примера
make test           # Запуск тестов
make run-example    # Запуск echo сервера
make fmt            # Форматирование кода
make clean          # Очистка
```

### Запуск примеров
```bash
# Echo сервер
./example/echo-server -addr :8080 -max-conn 100

# Тестирование
telnet localhost 8080
echo "Hello" | nc localhost 8080
```

## Расширение

### Создание кастомного протокола
1. Определите тип данных (struct)
2. Реализуйте ProtocolParser[YourType]
3. Создайте Server[YourType]
4. Определите обработчики для YourType

### Создание кастомного логгера
1. Реализуйте интерфейс Logger
2. Передайте в Config.Logger

## Тестирование
Все компоненты покрыты unit тестами:
```bash
go test -v ./pkg/tcpserver/
```

## Требования
- Go 1.25+ (для generics и других фич)
- Стандартная библиотека (нет внешних зависимостей)
