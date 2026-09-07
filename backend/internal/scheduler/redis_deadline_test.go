package scheduler

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/config"
	"github.com/DevilGenius/airgate-core/internal/redisconfig"
	"github.com/redis/go-redis/v9"
)

func TestRedisAdmissionDeadlineWhenPeerNeverResponds(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		accepted <- conn
		_, _ = io.Copy(io.Discard, conn)
	}()
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	rdb := redis.NewClient(redisconfig.Options(config.RedisConfig{Host: "127.0.0.1", Port: port}))
	defer rdb.Close()
	started := time.Now()
	allowed, err := NewRPMCounter(rdb).TryIncrementRPM(context.Background(), 7, 10)
	if allowed || !errors.Is(err, ErrSchedulingUnavailable) {
		t.Fatalf("unresponsive peer admitted: %v, %v", allowed, err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("admission took %v", elapsed)
	}
	select {
	case conn := <-accepted:
		_ = conn.Close()
	default:
		t.Fatal("test did not establish a connection")
	}
}
