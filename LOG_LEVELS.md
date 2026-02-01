# Уровни логирования в rltcpkit

## Обзор

Библиотека rltcpkit поддерживает 4 уровня детализации логирования через параметр `LogLevel` в конфигурации. Это позволяет гибко контролировать объём логов в зависимости от ваших потребностей.

**Важно:** `LogLevel` влияет **только на debug логи**. Логи уровня `Info`, `Warn` и `Error` выводятся всегда, независимо от настройки `LogLevel`.

## Уровни логирования

### LogLevelInfo (0) - Production режим
**По умолчанию**. Отключает все debug логи.

**Что логируется:**
- ✅ Запуск/остановка сервера/клиента (Info)
- ✅ Принятие новых соединений (Info)
- ✅ Graceful shutdown события (Info)
- ✅ Все ошибки (Error)
- ✅ Предупреждения (Warn)
- ❌ Debug логи отключены

**Пример использования:**
```go
config := rltcpkit.Config{
    MaxConnections: 100,
    Logger: logger,
    LogLevel: rltcpkit.LogLevelInfo, // или просто не указывать
}
```

### LogLevelDebug1 (1) - Основные debug события
Добавляет логирование основных событий соединений.

**Дополнительно логируется:**
- Закрытие соединений
- Context cancelled события
- Завершение read/write горутин
- Socket closed by remote peer
- Connection cleanup

**Пример использования:**
```go
config := rltcpkit.Config{
    MaxConnections: 100,
    Logger: logger,
    LogLevel: rltcpkit.LogLevelDebug1,
}
```

### LogLevelDebug2 (2) - Детали соединений
Добавляет подробное логирование внутренней работы соединений.

**Дополнительно логируется (к Debug1):**
- EventLoop переходы между состояниями
- Вызовы обработчиков (OnRead, OnStop, OnError, OnConnected)
- Завершение обработчиков
- Отправка ошибок в errorChan
- Детали graceful shutdown процесса
- Состояния буферов каналов

**Пример использования:**
```go
config := rltcpkit.Config{
    MaxConnections: 100,
    Logger: logger,
    LogLevel: rltcpkit.LogLevelDebug2,
}
```

### LogLevelDebug3 (3) - Максимальная детализация + пакеты
⚠️ **ВНИМАНИЕ**: Генерирует большой объём логов!

**Дополнительно логируется (к Debug1 + Debug2):**
- 📦 Отправка каждого пакета: `Packet sent`, conn_id, direction=sent, bytes
- 📦 Получение каждого пакета: `Packet received`, conn_id, direction=received, bytes

**Пример использования:**
```go
config := rltcpkit.Config{
    MaxConnections: 100,
    Logger: logger,
    LogLevel: rltcpkit.LogLevelDebug3,
}
```

## Использование с Server

```go
package main

import (
    "log/slog"
    "os"
    "github.com/example/rltcpkit/pkg/rltcpkit"
)

func main() {
    // Создаём slog logger с уровнем Debug
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug, // Важно: установить минимальный уровень на Debug
    }))

    // Создаём сервер с Debug Level 2
    server := rltcpkit.NewServer[[]byte](":8080", rltcpkit.Config{
        MaxConnections: 100,
        Logger: logger,
        LogLevel: rltcpkit.LogLevelDebug2, // Детали соединений
    })
    
    // ... остальной код
}
```

## Использование с Client

```go
package main

import (
    "log/slog"
    "os"
    "github.com/example/rltcpkit/pkg/rltcpkit"
)

func main() {
    // Создаём slog logger с уровнем Debug
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    }))

    // Создаём клиент с максимальной отладкой (включая пакеты)
    client := rltcpkit.NewClient[[]byte]("localhost:8080", rltcpkit.ClientConfig{
        ConnectTimeout: 5 * time.Second,
        ReconnectEnabled: true,
        Logger: logger,
        LogLevel: rltcpkit.LogLevelDebug3, // Включая логи пакетов
    })
    
    // ... остальной код
}
```

## Пример запуска Echo сервера с разными уровнями

```bash
# Уровень Info (по умолчанию) - только важные события
./example/echo-server -addr :8080 -log-level 0

# Уровень Debug1 - основные debug события
./example/echo-server -addr :8080 -log-level 1

# Уровень Debug2 - детали соединений и обработчиков
./example/echo-server -addr :8080 -log-level 2

# Уровень Debug3 - максимум (включая все отправляемые/получаемые пакеты)
./example/echo-server -addr :8080 -log-level 3
```

## Примеры вывода логов

### LogLevel = Info (0)
```
INFO TCP server started address=:8080
INFO New connection remote_addr=127.0.0.1:54321
INFO Connection closed conn_id=1 remote_addr=127.0.0.1:54321
INFO TCP server stopped
```

### LogLevel = Debug1 (1)
```
INFO TCP server started address=:8080
INFO New connection remote_addr=127.0.0.1:54321
DEBUG Read loop closed conn_id=1 remote_addr=127.0.0.1:54321
DEBUG Write loop closed conn_id=1 remote_addr=127.0.0.1:54321
DEBUG Event loop finished conn_id=1 remote_addr=127.0.0.1:54321
DEBUG Event loop closed conn_id=1 remote_addr=127.0.0.1:54321
INFO Connection closed conn_id=1 remote_addr=127.0.0.1:54321
INFO TCP server stopped
```

### LogLevel = Debug2 (2)
```
INFO TCP server started address=:8080
INFO New connection remote_addr=127.0.0.1:54321
DEBUG EventLoop waiting for events conn_id=1 shutdown_pending=false
DEBUG Received packet from readChan conn_id=1 ok=true
DEBUG Calling OnRead handler conn_id=1
DEBUG OnRead handler completed conn_id=1
DEBUG EventLoop waiting for events conn_id=1 shutdown_pending=false
DEBUG Received shutdown signal conn_id=1
DEBUG Calling OnStop handler conn_id=1
DEBUG OnStop handler completed conn_id=1
DEBUG Setting shutdownCh to nil conn_id=1
DEBUG Read loop closed conn_id=1 remote_addr=127.0.0.1:54321
DEBUG Write loop closed conn_id=1 remote_addr=127.0.0.1:54321
DEBUG Event loop finished conn_id=1 remote_addr=127.0.0.1:54321
DEBUG Event loop closed conn_id=1 remote_addr=127.0.0.1:54321
INFO Connection closed conn_id=1 remote_addr=127.0.0.1:54321
INFO TCP server stopped
```

### LogLevel = Debug3 (3)
```
INFO TCP server started address=:8080
INFO New connection remote_addr=127.0.0.1:54321
DEBUG EventLoop waiting for events conn_id=1 shutdown_pending=false
DEBUG Packet received conn_id=1 direction=received bytes=12
DEBUG Received packet from readChan conn_id=1 ok=true
DEBUG Calling OnRead handler conn_id=1
DEBUG Packet sent conn_id=1 direction=sent bytes=18
DEBUG OnRead handler completed conn_id=1
DEBUG EventLoop waiting for events conn_id=1 shutdown_pending=false
... (все пакеты логируются)
```

## Рекомендации

### Production
- Используйте `LogLevelInfo` (0)
- Минимальный объём логов
- Только важные события

### Development
- Используйте `LogLevelDebug1` (1) или `LogLevelDebug2` (2)
- Достаточно информации для отладки
- Приемлемый объём логов

### Глубокая отладка протокола
- Используйте `LogLevelDebug3` (3)
- Только для анализа проблем с протоколом
- Очень большой объём логов

### Настройка slog Handler
**Важно**: Убедитесь, что `slog.HandlerOptions.Level` установлен на `slog.LevelDebug`, иначе debug логи будут отфильтрованы ещё до проверки `LogLevel`:

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug, // Обязательно для работы LogLevel
}))
```

## Обратная совместимость

- По умолчанию `LogLevel = LogLevelInfo` (0)
- Существующий код без указания LogLevel продолжит работать как раньше
- `LogLevel` - опциональное поле в Config
