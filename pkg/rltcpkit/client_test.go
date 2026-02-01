package rltcpkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newConsoleLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestClientConnect проверяет успешное подключение клиента к серверу.
func TestClientConnect(t *testing.T) {
	// Создаем сервер
	server := NewServer[[]byte](":0", Config{
		Logger: newConsoleLogger(),
	})

	parser := NewByteParser()
	serverDone, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{
			OnRead: func(ctx context.Context, c *Connection[[]byte], data []byte) {
				// Echo server
				_ = c.Write(ctx, data)
			},
		}
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		_ = server.Stop()
		<-serverDone
	}()

	// Создаем клиент
	client := NewClient[[]byte](server.GetAddress(), ClientConfig{
		ConnectTimeout:   5 * time.Second,
		ReconnectEnabled: false,
		Logger:           newConsoleLogger(),
	})

	var connected atomic.Bool
	connectedCh := make(chan struct{})
	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
		OnConnected: func(conn *Connection[[]byte]) {
			connected.Store(true)
			close(connectedCh)
		},
	})
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	defer func() {
		_ = client.Stop()
		<-done
	}()

	// Ждем подключения
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	// Проверяем что клиент подключен
	if !client.IsConnected() {
		t.Error("Client should be connected")
	}

	if !connected.Load() {
		t.Error("OnConnected should be called")
	}
}

// TestClientWriteRead проверяет отправку и получение данных.
func TestClientWriteRead(t *testing.T) {
	// Создаем сервер
	server := NewServer[[]byte](":0", Config{
		Logger: newConsoleLogger(),
	})

	parser := NewByteParser()
	serverDone, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{
			OnRead: func(ctx context.Context, c *Connection[[]byte], data []byte) {
				// Echo server
				_ = c.Write(ctx, data)
			},
		}
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		_ = server.Stop()
		<-serverDone
	}()

	// Создаем клиент
	client := NewClient[[]byte](server.GetAddress(), ClientConfig{
		ConnectTimeout:   5 * time.Second,
		ReconnectEnabled: false,
		Logger:           newConsoleLogger(),
	})

	var receivedData []byte
	var receivedMu sync.Mutex
	receivedCh := make(chan struct{})
	connectedCh := make(chan struct{})

	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
		OnConnected: func(conn *Connection[[]byte]) {
			close(connectedCh)
		},
		OnRead: func(ctx context.Context, conn *Connection[[]byte], data []byte) {
			receivedMu.Lock()
			receivedData = data
			receivedMu.Unlock()
			close(receivedCh)
		},
	})
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	defer func() {
		_ = client.Stop()
		<-done
	}()

	// Ждем подключения
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	// Отправляем данные
	testData := []byte("Hello, Server!")
	err = client.Write(context.Background(), testData)
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Ждем ответ
	select {
	case <-receivedCh:
		receivedMu.Lock()
		if string(receivedData) != string(testData) {
			t.Errorf("Expected %s, got %s", testData, receivedData)
		}
		receivedMu.Unlock()
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

// TestClientDisconnect проверяет корректное отключение клиента.
func TestClientDisconnect(t *testing.T) {
	// Создаем сервер
	server := NewServer[[]byte](":0", Config{
		Logger: newConsoleLogger(),
	})

	parser := NewByteParser()
	serverDone, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{}
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		_ = server.Stop()
		<-serverDone
	}()

	// Создаем клиент
	client := NewClient[[]byte](server.GetAddress(), ClientConfig{
		ConnectTimeout:   5 * time.Second,
		ReconnectEnabled: false,
		Logger:           newConsoleLogger(),
	})

	var disconnected atomic.Bool
	connectedCh := make(chan struct{})
	disconnectedCh := make(chan struct{})
	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
		OnConnected: func(conn *Connection[[]byte]) {
			close(connectedCh)
		},
		OnDisconnected: func(conn *Connection[[]byte], err error) {
			disconnected.Store(true)
			close(disconnectedCh)
		},
	})
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	// Ждем подключения
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	// Отключаемся
	err = client.Stop()
	if err != nil {
		t.Fatalf("Failed to stop: %v", err)
	}
	<-done

	// Проверяем что клиент отключен
	if client.IsConnected() {
		t.Error("Client should be disconnected")
	}

	// Ждем вызова OnDisconnected
	select {
	case <-disconnectedCh:
	case <-time.After(1 * time.Second):
		t.Error("OnDisconnected should be called")
		return
	}

	if !disconnected.Load() {
		t.Error("OnDisconnected should be called")
	}
}

// TestClientReconnect проверяет автоматическое переподключение.
func TestClientReconnect(t *testing.T) {
	// Создаем сервер
	server := NewServer[[]byte](":0", Config{
		Logger: newConsoleLogger(),
	})

	parser := NewByteParser()
	serverDone, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{}
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	serverAddr := server.GetAddress()

	// Создаем клиент с автоматическим переподключением
	client := NewClient[[]byte](serverAddr, ClientConfig{
		ConnectTimeout:       5 * time.Second,
		ReconnectEnabled:     true,
		MaxReconnectAttempts: 5,
		ReconnectBaseDelay:   100 * time.Millisecond,
		ReconnectMaxDelay:    1 * time.Second,
		Logger:               newConsoleLogger(),
	})

	var connectedCount atomic.Int32
	var disconnectedCount atomic.Int32
	var reconnectingCount atomic.Int32
	initialConnectedCh := make(chan struct{})

	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
		OnConnected: func(conn *Connection[[]byte]) {
			count := connectedCount.Add(1)
			if count == 1 {
				close(initialConnectedCh)
			}
		},
		OnDisconnected: func(conn *Connection[[]byte], err error) {
			disconnectedCount.Add(1)
		},
		OnReconnecting: func(attempt int) {
			reconnectingCount.Add(1)
		},
	})
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	defer func() {
		_ = client.Stop()
		<-done
	}()

	// Ждем первого подключения
	select {
	case <-initialConnectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for initial connection")
	}

	// Проверяем первое подключение
	if connectedCount.Load() != 1 {
		t.Errorf("Expected 1 connection, got %d", connectedCount.Load())
	}

	// Останавливаем сервер чтобы вызвать разрыв
	_ = server.Stop()
	<-serverDone

	// Ждем обнаружения разрыва
	time.Sleep(500 * time.Millisecond)

	// Проверяем что был вызван OnDisconnected
	if disconnectedCount.Load() < 1 {
		t.Error("OnDisconnected should be called")
	}

	// Проверяем что начались попытки переподключения
	time.Sleep(1 * time.Second)
	if reconnectingCount.Load() < 1 {
		t.Error("OnReconnecting should be called")
	}

	// Запускаем сервер снова
	server = NewServer[[]byte](serverAddr, Config{
		Logger: newConsoleLogger(),
	})
	serverDone, err = server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{}
	})
	if err != nil {
		t.Fatalf("Failed to restart server: %v", err)
	}
	defer func() {
		_ = server.Stop()
		<-serverDone
	}()

	// Ждем переподключения
	time.Sleep(3 * time.Second)

	// Проверяем что клиент переподключился
	if !client.IsConnected() {
		t.Error("Client should reconnect")
	}

	if connectedCount.Load() < 2 {
		t.Errorf("Expected at least 2 connections, got %d", connectedCount.Load())
	}
}

// TestClientMaxReconnectAttempts проверяет достижение максимума попыток переподключения.
func TestClientMaxReconnectAttempts(t *testing.T) {
	// Создаем сервер
	server := NewServer[[]byte](":0", Config{
		Logger: newConsoleLogger(),
	})

	parser := NewByteParser()
	serverDone, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{}
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	serverAddr := server.GetAddress()

	// Создаем клиент с ограниченными попытками переподключения
	client := NewClient[[]byte](serverAddr, ClientConfig{
		ConnectTimeout:       5 * time.Second,
		ReconnectEnabled:     true,
		MaxReconnectAttempts: 3,
		ReconnectBaseDelay:   100 * time.Millisecond,
		ReconnectMaxDelay:    500 * time.Millisecond,
		Logger:               newConsoleLogger(),
	})

	var reconnectingCount atomic.Int32
	var finalError atomic.Bool
	connectedCh := make(chan struct{})
	finalErrorCh := make(chan struct{})

	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
		OnConnected: func(conn *Connection[[]byte]) {
			close(connectedCh)
		},
		OnError: func(conn *Connection[[]byte], err error) {
			if errors.Is(err, ErrReconnectFailed) {
				finalError.Store(true)
				close(finalErrorCh)
			}
		},
		OnReconnecting: func(attempt int) {
			reconnectingCount.Add(1)
		},
	})
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	// Ждем подключения
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	// Останавливаем сервер
	_ = server.Stop()
	<-serverDone

	// Ждем исчерпания попыток переподключения и финальной ошибки
	select {
	case <-finalErrorCh:
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for OnError with ErrReconnectFailed")
		_ = client.Stop()
		<-done
		return
	}

	// Проверяем что было сделано 3 попытки
	attempts := reconnectingCount.Load()
	if attempts != 3 {
		t.Errorf("Expected 3 reconnect attempts, got %d", attempts)
	}

	// Проверяем что был вызван OnError с ErrReconnectFailed
	if !finalError.Load() {
		t.Error("OnError should be called with ErrReconnectFailed")
	}

	_ = client.Stop()
	<-done
}

// TestClientContextCancellation проверяет отмену через контекст.
func TestClientContextCancellation(t *testing.T) {
	// Создаем сервер
	server := NewServer[[]byte](":0", Config{
		Logger: newConsoleLogger(),
	})

	parser := NewByteParser()
	serverDone, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{}
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		_ = server.Stop()
		<-serverDone
	}()

	// Создаем клиент
	client := NewClient[[]byte](server.GetAddress(), ClientConfig{
		ConnectTimeout:   5 * time.Second,
		ReconnectEnabled: false,
		Logger:           newConsoleLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())

	connectedCh := make(chan struct{})
	done, err := client.Start(ctx, parser, ClientHandlers[[]byte]{
		OnConnected: func(conn *Connection[[]byte]) {
			close(connectedCh)
		},
	})
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	// Ждем подключения
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	// Отменяем контекст
	cancel()

	// Ждем что connectionLoop завершится
	select {
	case <-done:
		// connectionLoop завершился
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for client to stop after context cancellation")
		return
	}

	// Явно вызываем Stop() чтобы закрыть соединение
	_ = client.Stop()

	// Попытка записи должна завершиться с ошибкой т.к. клиент остановлен
	err = client.Write(context.Background(), []byte("test"))
	if err == nil {
		t.Error("Write should fail after Stop")
	}
}

// TestClientWriteNotConnected проверяет ошибку при записи без подключения.
func TestClientWriteNotConnected(t *testing.T) {
	client := NewClient[[]byte]("localhost:9999", ClientConfig{
		ConnectTimeout: 5 * time.Second,
		Logger:         newConsoleLogger(),
	})

	err := client.Write(context.Background(), []byte("test"))
	if !errors.Is(err, ErrClientNotConnected) {
		t.Errorf("Expected ErrClientNotConnected, got %v", err)
	}
}

// TestClientConnectTimeout проверяет таймаут подключения.
func TestClientConnectTimeout(t *testing.T) {
	// Используем недоступный адрес
	client := NewClient[[]byte]("192.0.2.1:9999", ClientConfig{
		ConnectTimeout:   100 * time.Millisecond,
		ReconnectEnabled: false,
		Logger:           newConsoleLogger(),
	})

	parser := NewByteParser()
	errorOccurred := make(chan struct{})
	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
		OnError: func(conn *Connection[[]byte], err error) {
			if errors.Is(err, ErrConnectionFailed) {
				close(errorOccurred)
			}
		},
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() {
		_ = client.Stop()
		<-done
	}()

	// Ждем ошибки подключения
	select {
	case <-errorOccurred:
		// Успех - подключение завершилось с таймаутом
	case <-time.After(2 * time.Second):
		t.Error("Should receive connection error due to timeout")
	}

	// Проверяем что клиент не подключен
	if client.IsConnected() {
		t.Error("Client should not be connected")
	}
}

// TestClientGetConnection проверяет получение текущего соединения.
func TestClientGetConnection(t *testing.T) {
	// Создаем сервер
	server := NewServer[[]byte](":0", Config{
		Logger: newConsoleLogger(),
	})

	parser := NewByteParser()
	serverDone, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{}
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		_ = server.Stop()
		<-serverDone
	}()

	// Создаем клиент
	client := NewClient[[]byte](server.GetAddress(), ClientConfig{
		ConnectTimeout:   5 * time.Second,
		ReconnectEnabled: false,
		Logger:           newConsoleLogger(),
	})

	// Проверяем что соединения нет
	if client.GetConnection() != nil {
		t.Error("Connection should be nil before connect")
	}

	connectedCh := make(chan struct{})
	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
		OnConnected: func(conn *Connection[[]byte]) {
			close(connectedCh)
		},
	})
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	defer func() {
		_ = client.Stop()
		<-done
	}()

	// Ждем подключения
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	// Проверяем что соединение есть
	conn := client.GetConnection()
	if conn == nil {
		t.Error("Connection should not be nil after connect")
	}

	// Проверяем что можно использовать соединение напрямую
	err = conn.Write(context.Background(), []byte("test"))
	if err != nil {
		t.Errorf("Failed to write through connection: %v", err)
	}
}

// TestClientAlreadyStarted проверяет ошибку при повторном запуске.
func TestClientAlreadyStarted(t *testing.T) {
	// Создаем сервер
	server := NewServer[[]byte](":0", Config{
		Logger: newConsoleLogger(),
	})

	parser := NewByteParser()
	serverDone, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{}
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		_ = server.Stop()
		<-serverDone
	}()

	// Создаем клиент
	client := NewClient[[]byte](server.GetAddress(), ClientConfig{
		ConnectTimeout:   5 * time.Second,
		ReconnectEnabled: false,
		Logger:           newConsoleLogger(),
	})

	clientDone, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{})
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	defer func() {
		_ = client.Stop()
		<-clientDone
	}()

	// Пытаемся запустить снова
	_, err = client.Start(context.Background(), parser, ClientHandlers[[]byte]{})
	if !errors.Is(err, ErrClientAlreadyStarted) {
		t.Errorf("Expected ErrClientAlreadyStarted, got %v", err)
	}
}

// TestClientServerDisconnect проверяет обработку разрыва соединения сервером.
func TestClientServerDisconnect(t *testing.T) {
	// Создаем сервер
	server := NewServer[[]byte](":0", Config{
		Logger: newConsoleLogger(),
	})

	parser := NewByteParser()
	serverDone, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{
			OnRead: func(ctx context.Context, c *Connection[[]byte], data []byte) {
				// Закрываем соединение при получении данных
				_ = c.Close(true)
			},
		}
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		_ = server.Stop()
		<-serverDone
	}()

	client := NewClient[[]byte](server.GetAddress(), ClientConfig{
		ConnectTimeout:   5 * time.Second,
		ReconnectEnabled: false,
		Logger:           newConsoleLogger(),
	})

	var disconnectOccurred atomic.Bool
	connectedCh := make(chan struct{})

	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
		OnConnected: func(conn *Connection[[]byte]) {
			close(connectedCh)
		},
		OnDisconnected: func(conn *Connection[[]byte], err error) {
			disconnectOccurred.Store(true)
		},
	})
	if err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	defer func() {
		_ = client.Stop()
		<-done
	}()

	// Ждем подключения
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	// Отправляем данные чтобы спровоцировать закрытие соединения сервером
	err = client.Write(context.Background(), []byte("test"))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Ждем обнаружения разрыва соединения
	timeout := time.After(2 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

waitLoop:
	for {
		select {
		case <-timeout:
			break waitLoop
		case <-ticker.C:
			if disconnectOccurred.Load() {
				break waitLoop
			}
		}
	}

	if !disconnectOccurred.Load() {
		t.Error("OnDisconnected should be called when server closes connection")
	}
}

// BenchmarkClientWriteRead бенчмарк для записи и чтения данных.
func BenchmarkClientWriteRead(b *testing.B) {
	// Создаем сервер
	server := NewServer[[]byte](":0", Config{
		Logger: newConsoleLogger(),
	})

	parser := NewByteParser()
	serverDone, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{
			OnRead: func(ctx context.Context, c *Connection[[]byte], data []byte) {
				_ = c.Write(ctx, data)
			},
		}
	})
	if err != nil {
		b.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		_ = server.Stop()
		<-serverDone
	}()

	// Создаем клиент
	client := NewClient[[]byte](server.GetAddress(), ClientConfig{
		ConnectTimeout:   5 * time.Second,
		ReconnectEnabled: false,
		Logger:           newConsoleLogger(),
	})

	receivedCh := make(chan struct{}, b.N)
	connectedCh := make(chan struct{})

	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
		OnConnected: func(conn *Connection[[]byte]) {
			close(connectedCh)
		},
		OnRead: func(ctx context.Context, conn *Connection[[]byte], data []byte) {
			receivedCh <- struct{}{}
		},
	})
	if err != nil {
		b.Fatalf("Failed to start: %v", err)
	}
	defer func() {
		_ = client.Stop()
		<-done
	}()

	// Ждем подключения
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		b.Fatal("Timeout waiting for connection")
	}

	testData := []byte("benchmark test data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = client.Write(context.Background(), testData)
		if err != nil {
			b.Fatalf("Failed to write: %v", err)
		}
		<-receivedCh
	}
}

// ExampleClient демонстрирует базовое использование клиента.
func ExampleClient() {
	// Создаем клиент
	client := NewClient[[]byte]("localhost:8080", ClientConfig{
		ConnectTimeout:       5 * time.Second,
		ReconnectEnabled:     true,
		MaxReconnectAttempts: 5,
		ReconnectBaseDelay:   1 * time.Second,
		ReconnectMaxDelay:    30 * time.Second,
	})

	// Подключаемся к серверу
	parser := NewByteParser()
	done, err := client.Start(context.Background(), parser, ClientHandlers[[]byte]{
		OnConnected: func(conn *Connection[[]byte]) {
			fmt.Println("Connected to server!")
		},
		OnRead: func(ctx context.Context, conn *Connection[[]byte], data []byte) {
			fmt.Printf("Received: %s\n", data)
		},
		OnError: func(conn *Connection[[]byte], err error) {
			fmt.Printf("Error: %v\n", err)
		},
	})
	if err != nil {
		fmt.Printf("Failed to start: %v\n", err)
		return
	}
	defer func() {
		_ = client.Stop()
		<-done
	}()

	// Отправляем данные
	_ = client.Write(context.Background(), []byte("Hello, Server!"))
}
