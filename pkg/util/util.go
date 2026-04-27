package util

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
)

var (
	logger = log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
)

func Infof(format string, args ...interface{}) {
	logger.Output(2, "[INFO] "+fmt.Sprintf(format, args...))
}

func Warnf(format string, args ...interface{}) {
	logger.Output(2, "[WARN] "+fmt.Sprintf(format, args...))
}

func Errorf(format string, args ...interface{}) {
	logger.Output(2, "[ERROR] "+fmt.Sprintf(format, args...))
}

func RandomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Bridge melakukan io.Copy dua arah antara dua koneksi.
// Selesai ketika salah satu sisi tutup atau error.
func Bridge(a, b io.ReadWriteCloser) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(a, b)
		if c, ok := a.(closeWriter); ok {
			_ = c.CloseWrite()
		} else {
			_ = a.Close()
		}
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(b, a)
		if c, ok := b.(closeWriter); ok {
			_ = c.CloseWrite()
		} else {
			_ = b.Close()
		}
	}()

	wg.Wait()
	_ = a.Close()
	_ = b.Close()
}

type closeWriter interface {
	CloseWrite() error
}

// JoinHostPort wrapper.
func JoinHostPort(host string, port int) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}
