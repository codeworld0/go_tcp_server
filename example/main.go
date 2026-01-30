package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/rltcpkit/pkg/rltcpkit"
)

func main() {
	// Параметры командной строки
	var (
		address        = flag.String("addr", ":8080", "Address to listen on (default :8080)")
		maxConnections = flag.Int("max-conn", 100, "Maximum number of connections (default 100)")
		shutdownTime   = flag.Int("shutdown", 5, "Graceful shutdown timeout in seconds (default 5)")
	)
	flag.Parse()

	// Создаем логгер
	logger := &SimpleLogger{}

	// Создаем конфигурацию сервера
	config := rltcpkit.Config{
		MaxConnections: *maxConnections,
		Logger:         logger,
	}

	// Создаем сервер
	server := rltcpkit.NewServer[[]byte](*address, config)

	// Устанавливаем graceful shutdown timeout
	shutdownTimeout := time.Duration(*shutdownTime) * time.Second
	server.SetGracefulTimeout(shutdownTimeout)

	// Создаем парсер для байтовых данных
	parser := rltcpkit.NewByteParser()

	// Статистика
	var connectionCounter int

	// Запускаем сервер
	logger.Info("Starting Echo TCP Server on %s", *address)
	logger.Info("Max connections: %d", *maxConnections)
	logger.Info("Graceful shutdown timeout: %v", shutdownTimeout)

	done, err := server.Start(context.Background(), parser, func(conn *rltcpkit.Connection[[]byte]) rltcpkit.ConnectionHandlers[[]byte] {
		// Увеличиваем счетчик подключений
		connectionCounter++
		connID := connectionCounter

		logger.Info("Connection #%d accepted from %s", connID, conn.RemoteAddr())
		conn.SetDeadline(time.Time{})
		// Отправляем приветственное сообщение
		welcomeMsg := fmt.Sprintf("Welcome to Echo Server! You are connection #%d\n", connID)
		conn.Write(context.Background(), []byte(welcomeMsg))

		// Возвращаем обработчики для этого соединения
		return rltcpkit.ConnectionHandlers[[]byte]{
			OnRead: func(ctx context.Context, c *rltcpkit.Connection[[]byte], data []byte) {
				logger.Info("Connection #%d received %d bytes: %s", connID, len(data), string(data))

				// Echo: отправляем данные обратно
				response := fmt.Sprintf("Echo: %s", string(data))
				err := c.Write(ctx, []byte(response))
				if err != nil {
					logger.Error("Connection #%d write error: %v", connID, err)
				}
			},

			OnError: func(c *rltcpkit.Connection[[]byte], err error) {
				logger.Error("Connection #%d error: %v", connID, err)
			},

			OnStop: func(c *rltcpkit.Connection[[]byte]) {
				logger.Info("Connection #%d stopping gracefully...", connID)
				// Отправляем прощальное сообщение
				c.Write(context.Background(), []byte("Server is shutting down. Goodbye!\n"))
				c.Close(false)
			},

			OnClosed: func(c *rltcpkit.Connection[[]byte]) {
				logger.Info("Connection #%d closed (from %s)", connID, c.RemoteAddr())
			},
		}
	})

	if err != nil {
		logger.Error("Failed to start server: %v", err)
		os.Exit(1)
	}

	// Ожидаем сигнал завершения
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	logger.Info("Server is running. Press Ctrl+C to stop.")
	logger.Info("You can test it with: telnet localhost %s", *address)

	<-sigChan
	logger.Info("Received shutdown signal")

	// Останавливаем сервер
	logger.Info("Stopping server...")

	err = server.Stop()
	if err != nil {
		logger.Error("Error during shutdown: %v", err)
		os.Exit(1)
	}

	// Ждем полной остановки сервера
	<-done
	logger.Info("Server stopped successfully. Total connections served: %d", connectionCounter)
}
