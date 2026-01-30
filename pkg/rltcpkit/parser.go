package rltcpkit

import (
	"context"
	"io"
	"net"
)

// ProtocolParser определяет интерфейс для чтения и записи пакетов через TCP соединение.
// Параметр типа T представляет тип данных протокола (например, []byte, структура сообщения и т.д.).
//
// Этот интерфейс позволяет создавать различные реализации протоколов,
// от простых байтовых потоков до сложных бинарных или текстовых протоколов.
type ProtocolParser[T any] interface {
	// ReadPacket читает один пакет данных из соединения.
	// Метод должен блокироваться до получения полного пакета или возникновения ошибки.
	//
	// Параметры:
	//   - ctx: контекст для отмены операции чтения
	//   - conn: TCP соединение для чтения данных
	//
	// Возвращает:
	//   - T: прочитанный пакет данных
	//   - error: ошибка чтения или nil при успешном чтении
	ReadPacket(ctx context.Context, conn net.Conn) (T, error)

	// WritePacket записывает один пакет данных в соединение.
	// Метод должен блокироваться до полной отправки пакета или возникновения ошибки.
	// Отмена операции происходит через закрытие сокета на верхнем уровне.
	//
	// Параметры:
	//   - conn: TCP соединение для записи данных
	//   - packet: пакет данных для отправки
	//
	// Возвращает:
	//   - error: ошибка записи или nil при успешной записи
	WritePacket(conn net.Conn, packet T) error
}

// ByteParser реализует ProtocolParser для работы с сырыми байтами.
// Это простейшая реализация протокола, которая читает и пишет данные
// фиксированными блоками или до конца потока.
//
// ByteParser читает данные блоками по bufferSize байт.
// Если bufferSize == 0, используется значение по умолчанию (4096 байт).
type ByteParser struct {
	bufferSize int
}

// NewByteParser создает новый ByteParser с размером буфера по умолчанию (4096 байт).
// Это наиболее распространенный вариант использования для простых байтовых протоколов.
func NewByteParser() *ByteParser {
	return &ByteParser{
		bufferSize: 4096,
	}
}

// NewByteParserWithBuffer создает новый ByteParser с указанным размером буфера.
// Используйте эту функцию, если вам нужен специфический размер буфера
// для оптимизации производительности или работы с конкретным протоколом.
//
// Параметры:
//   - bufferSize: размер буфера для чтения данных (должен быть > 0)
func NewByteParserWithBuffer(bufferSize int) *ByteParser {
	if bufferSize <= 0 {
		bufferSize = 4096
	}
	return &ByteParser{
		bufferSize: bufferSize,
	}
}

// ReadPacket читает данные из соединения в буфер указанного размера.
// Возвращает прочитанные байты или ошибку.
//
// Важно: метод читает до bufferSize байт или до конца потока (EOF).
// Если нужно прочитать точное количество байт, используйте кастомную реализацию ProtocolParser.
func (b *ByteParser) ReadPacket(ctx context.Context, conn net.Conn) ([]byte, error) {
	buffer := make([]byte, b.bufferSize)

	// Проверяем, не отменен ли контекст
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	n, err := conn.Read(buffer)
	if err != nil {
		if err == io.EOF {
			return nil, err
		}
		return nil, err
	}

	// Возвращаем только реально прочитанные байты
	return buffer[:n], nil
}

// WritePacket записывает данные в соединение.
// Гарантирует запись всех байт из packet или возвращает ошибку.
func (b *ByteParser) WritePacket(conn net.Conn, packet []byte) error {
	// Записываем все данные
	totalWritten := 0
	for totalWritten < len(packet) {
		n, err := conn.Write(packet[totalWritten:])
		if err != nil {
			return err
		}
		totalWritten += n
	}

	return nil
}
