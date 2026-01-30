package rltcpkit

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestServerStartStop проверяет базовый запуск и остановку сервера
func TestServerStartStop(t *testing.T) {
	logger := NewNoopLogger()
	config := Config{
		MaxConnections: 10,
		Logger:         logger,
	}

	server := NewServer[[]byte](":0", config) // :0 = случайный порт
	server.SetGracefulTimeout(1 * time.Second)

	parser := NewByteParser()

	// Запускаем сервер
	done, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{}
	})

	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	if !server.IsRunning() {
		t.Error("Server should be running")
	}

	// Останавливаем сервер
	err = server.Stop()
	if err != nil {
		t.Fatalf("Failed to stop server: %v", err)
	}

	// Ждем полной остановки
	<-done

	if server.IsRunning() {
		t.Error("Server should not be running after stop")
	}
}

// TestServerConnection проверяет подключение к серверу
func TestServerConnection(t *testing.T) {
	logger := NewNoopLogger()
	config := Config{
		MaxConnections: 10,
		Logger:         logger,
	}

	server := NewServer[[]byte](":0", config)
	server.SetGracefulTimeout(1 * time.Second)
	parser := NewByteParser()

	// Канал для уведомления о подключении
	connected := make(chan bool, 1)

	_, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		connected <- true
		return ConnectionHandlers[[]byte]{}
	})

	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Подключаемся к серверу
	addr := server.GetAddress()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Ждем уведомления о подключении
	select {
	case <-connected:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for connection")
	}

	// Проверяем счетчик подключений
	if server.GetConnectionCount() != 1 {
		t.Errorf("Expected 1 connection, got %d", server.GetConnectionCount())
	}
}

// TestServerEcho проверяет эхо-функциональность
func TestServerEcho(t *testing.T) {
	logger := NewNoopLogger()
	config := Config{
		MaxConnections: 10,
		Logger:         logger,
	}

	server := NewServer[[]byte](":0", config)
	server.SetGracefulTimeout(1 * time.Second)
	parser := NewByteParser()

	_, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{
			OnRead: func(ctx context.Context, c *Connection[[]byte], data []byte) {
				c.Write(ctx, data) // Echo
			},
		}
	})

	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Подключаемся к серверу
	addr := server.GetAddress()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Отправляем данные
	testData := []byte("Hello, Server!")
	_, err = conn.Write(testData)
	if err != nil {
		t.Fatalf("Failed to write data: %v", err)
	}

	// Читаем ответ
	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buffer)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	response := buffer[:n]
	if string(response) != string(testData) {
		t.Errorf("Expected %s, got %s", string(testData), string(response))
	}
}

// TestServerMaxConnections проверяет ограничение максимального количества подключений
func TestServerMaxConnections(t *testing.T) {
	logger := NewNoopLogger()
	config := Config{
		MaxConnections: 2, // Только 2 подключения
		Logger:         logger,
	}

	server := NewServer[[]byte](":0", config)
	server.SetGracefulTimeout(1 * time.Second)
	parser := NewByteParser()

	_, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{}
	})

	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	addr := server.GetAddress()

	// Создаем 2 подключения
	conn1, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn1.Close()

	time.Sleep(100 * time.Millisecond)

	conn2, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn2.Close()

	time.Sleep(100 * time.Millisecond)

	// Третье подключение должно быть отклонено
	conn3, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn3.Close()

	// Пробуем записать - должно быть сразу закрыто
	conn3.SetWriteDeadline(time.Now().Add(1 * time.Second))
	time.Sleep(100 * time.Millisecond)

	// Проверяем что не более 2 подключений
	count := server.GetConnectionCount()
	if count > 2 {
		t.Errorf("Expected max 2 connections, got %d", count)
	}
}

// TestByteParser проверяет работу ByteParser
func TestByteParser(t *testing.T) {
	parser := NewByteParser()

	// Создаем пару соединений
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	ctx := context.Background()
	testData := []byte("Test data")

	// Пишем в одном конце
	go func() {
		err := parser.WritePacket(client, testData)
		if err != nil {
			t.Errorf("WritePacket failed: %v", err)
		}
	}()

	// Читаем в другом
	received, err := parser.ReadPacket(ctx, server)
	if err != nil {
		t.Fatalf("ReadPacket failed: %v", err)
	}

	if string(received) != string(testData) {
		t.Errorf("Expected %s, got %s", string(testData), string(received))
	}
}

// TestConnectionUserData проверяет работу с пользовательскими данными
func TestConnectionUserData(t *testing.T) {
	logger := NewNoopLogger()
	config := Config{
		MaxConnections: 10,
		Logger:         logger,
	}

	server := NewServer[[]byte](":0", config)
	server.SetGracefulTimeout(1 * time.Second)
	parser := NewByteParser()

	userDataSet := make(chan bool, 1)

	_, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		// Устанавливаем пользовательские данные
		conn.SetUserData("test-user-data")

		// Проверяем, что данные установлены
		if conn.GetUserData() == "test-user-data" {
			userDataSet <- true
		}

		return ConnectionHandlers[[]byte]{}
	})

	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Подключаемся
	addr := server.GetAddress()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Ждем установки данных
	select {
	case <-userDataSet:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for user data to be set")
	}
}

// TestGracefulShutdown проверяет корректность graceful shutdown с новой архитектурой
func TestGracefulShutdown(t *testing.T) {
	logger := NewNoopLogger()
	config := Config{
		MaxConnections: 10,
		Logger:         logger,
	}

	server := NewServer[[]byte](":0", config)
	server.SetGracefulTimeout(2 * time.Second)
	parser := NewByteParser()

	onStopCalled := make(chan bool, 1)
	onClosedCalled := make(chan bool, 1)

	_, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{
			OnStop: func(c *Connection[[]byte]) {
				// OnStop вызывается при graceful shutdown
				onStopCalled <- true
				// Симулируем какую-то работу при shutdown
				time.Sleep(200 * time.Millisecond)
			},
			OnClosed: func(c *Connection[[]byte]) {
				// OnClosed вызывается после полного закрытия
				onClosedCalled <- true
			},
		}
	})

	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Подключаемся к серверу
	addr := server.GetAddress()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Даем время на установку соединения
	time.Sleep(100 * time.Millisecond)

	// Проверяем, что соединение активно
	if server.GetConnectionCount() != 1 {
		t.Errorf("Expected 1 connection, got %d", server.GetConnectionCount())
	}

	// Запускаем graceful shutdown
	go server.Stop()

	// Проверяем что OnStop был вызван
	select {
	case <-onStopCalled:
		// OK
	case <-time.After(3 * time.Second):
		t.Error("Timeout waiting for OnStop to be called")
	}

	// Проверяем что OnClosed был вызван после OnStop
	select {
	case <-onClosedCalled:
		// OK
	case <-time.After(3 * time.Second):
		t.Error("Timeout waiting for OnClosed to be called")
	}

	// Проверяем что счетчик подключений обновился
	if server.GetConnectionCount() != 0 {
		t.Errorf("Expected 0 connections after shutdown, got %d", server.GetConnectionCount())
	}
}

// TestEventLoopHandlesAllEvents проверяет что eventLoop обрабатывает все события корректно
func TestEventLoopHandlesAllEvents(t *testing.T) {
	logger := NewNoopLogger()
	config := Config{
		MaxConnections: 10,
		Logger:         logger,
	}

	server := NewServer[[]byte](":0", config)
	server.SetGracefulTimeout(1 * time.Second)
	parser := NewByteParser()

	onReadCalled := make(chan bool, 1)
	onErrorCalled := make(chan bool, 1)

	_, err := server.Start(context.Background(), parser, func(conn *Connection[[]byte]) ConnectionHandlers[[]byte] {
		return ConnectionHandlers[[]byte]{
			OnRead: func(ctx context.Context, c *Connection[[]byte], data []byte) {
				onReadCalled <- true
				// Симулируем ошибку после первого чтения
				c.Close(true)
			},
			OnError: func(c *Connection[[]byte], err error) {
				onErrorCalled <- true
			},
		}
	})

	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Подключаемся к серверу
	addr := server.GetAddress()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Отправляем данные
	testData := []byte("Test message")
	_, err = conn.Write(testData)
	if err != nil {
		t.Fatalf("Failed to write data: %v", err)
	}

	// Проверяем что OnRead был вызван
	select {
	case <-onReadCalled:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for OnRead to be called")
	}

	// Даем время на закрытие соединения и обновление счетчика
	time.Sleep(200 * time.Millisecond)

	// Проверяем что счетчик подключений обновился (cleanup был вызван из eventLoop)
	if server.GetConnectionCount() != 0 {
		t.Errorf("Expected 0 connections after close, got %d", server.GetConnectionCount())
	}
}
