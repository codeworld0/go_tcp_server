package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
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
		logLevel       = flag.Int("log-level", 0, "Log level: 0=Info, 1=Debug1, 2=Debug2, 3=Debug3 (default 0)")
	)
	flag.Parse()

	// Создаем логгер
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug, // Устанавливаем минимальный уровень slog на Debug
	}))

	// Преобразуем log level
	var rlLogLevel rltcpkit.LogLevel
	switch *logLevel {
	case 1:
		rlLogLevel = rltcpkit.LogLevelDebug1
	case 2:
		rlLogLevel = rltcpkit.LogLevelDebug2
	case 3:
		rlLogLevel = rltcpkit.LogLevelDebug3
	default:
		rlLogLevel = rltcpkit.LogLevelInfo
	}

	// Создаем конфигурацию сервера
	config := rltcpkit.Config{
		MaxConnections: *maxConnections,
		Logger:         logger,
		LogLevel:       rlLogLevel,
	}

	// Создаем сервер
	server := rltcpkit.NewServer[[]byte](*address, config)

	// Устанавливаем graceful shutdown timeout
	shutdownTimeout := time.Duration(*shutdownTime) * time.Second
	server.SetGracefulTimeout(shutdownTimeout)

	// Создаем парсер для байтовых данных
	parser := rltcpkit.NewByteParser()

	// Создаем обработчики соединений
	handlers := rltcpkit.ConnectionHandlers[[]byte]{
		OnConnected: func(ctx context.Context, conn *rltcpkit.Connection[[]byte]) {
			connID := conn.GetID()
			logger.Info("Connection accepted", "conn_id", connID, "remote_addr", conn.RemoteAddr())

			// Сбрасываем дедлайны
			conn.SetDeadline(time.Time{})

			// Отправляем приветственное сообщение
			welcomeMsg := fmt.Sprintf("Welcome to Echo Server! You are connection #%d\n", connID)
			conn.Write(ctx, []byte(welcomeMsg))
		},
		OnRead: func(ctx context.Context, c *rltcpkit.Connection[[]byte], data []byte) {
			connID := c.GetID()
			logger.Info("Data received", "conn_id", connID, "bytes", len(data), "data", string(data))

			// Echo: отправляем данные обратно
			response := fmt.Sprintf("Echo: %s", string(data))
			err := c.Write(ctx, []byte(response))
			if err != nil {
				logger.Error("Write error", "conn_id", connID, "error", err)
			}
		},
		OnError: func(c *rltcpkit.Connection[[]byte], err error) {
			connID := c.GetID()
			logger.Error("Connection error", "conn_id", connID, "error", err)
		},
		OnStop: func(c *rltcpkit.Connection[[]byte]) {
			connID := c.GetID()
			logger.Info("Connection stopping gracefully", "conn_id", connID)
			// Отправляем прощальное сообщение
			c.Write(context.Background(), []byte("Server is shutting down. Goodbye!\n"))
			c.Close(false)
		},
		OnClosed: func(c *rltcpkit.Connection[[]byte]) {
			connID := c.GetID()
			logger.Info("Connection closed", "conn_id", connID, "remote_addr", c.RemoteAddr())
		},
	}

	// Запускаем сервер
	logger.Info("Starting Echo TCP Server", "address", *address)
	logger.Info("Configuration", "max_connections", *maxConnections, "shutdown_timeout", shutdownTimeout, "log_level", rlLogLevel.String())

	done, err := server.Start(context.Background(), parser, handlers, nil)

	if err != nil {
		logger.Error("Failed to start server", "error", err)
		os.Exit(1)
	}

	// Ожидаем сигнал завершения
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	logger.Info("Server is running. Press Ctrl+C to stop.")
	logger.Info("Test connection", "command", fmt.Sprintf("telnet localhost %s", *address))

	<-sigChan
	logger.Info("Received shutdown signal")

	// Останавливаем сервер
	logger.Info("Stopping server")

	err = server.Stop()
	if err != nil {
		logger.Error("Error during shutdown", "error", err)
		os.Exit(1)
	}

	// Ждем полной остановки сервера
	<-done

	// Получаем финальное количество соединений из сервера
	logger.Info("Server stopped successfully", "total_connections", server.GetConnectionCount())
}
