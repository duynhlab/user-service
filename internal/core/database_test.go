package database

import (
	"context"
	"testing"
	"time"

	"github.com/duynhlab/user-service/config"
)

// TestConnect_ParseError verifies Connect returns an error when the DSN is
// invalid. An unknown sslmode makes pgxpool.ParseConfig reject the DSN before
// any network I/O happens.
func TestConnect_ParseError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database = config.DatabaseConfig{
		Host:           "localhost",
		Port:           "5432",
		Name:           "user",
		User:           "user",
		Password:       "secret",
		SSLMode:        "bogus",
		MaxConnections: 25,
	}
	if _, err := Connect(context.Background(), cfg); err == nil {
		t.Fatal("want parse error, got nil")
	}
}

// TestConnect_PingError verifies Connect returns an error when the pool cannot
// reach the database. Port 1 on loopback refuses the connection, so Ping fails.
// A valid config with MaxConnections>0 also exercises the pool-sizing branch.
func TestConnect_PingError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database = config.DatabaseConfig{
		Host:           "127.0.0.1",
		Port:           "1",
		Name:           "user",
		User:           "user",
		Password:       "secret",
		SSLMode:        "disable",
		MaxConnections: 25,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Connect(ctx, cfg); err == nil {
		t.Fatal("want ping error, got nil")
	}
}
