package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

const defaultTimeout = 90 * time.Second

type result struct {
	Timestamp       time.Time `json:"ts"`
	ConnectMs       float64   `json:"connect_ms"`
	Select1Ms       float64   `json:"select1_ms"`
	Select1WarmMs   float64   `json:"select1_warm_ms"`
	TotalMs         float64   `json:"total_ms"`
	ServerVersion   string    `json:"server_version,omitempty"`
	PostmasterStart string    `json:"postmaster_start,omitempty"`
	DBUptimeS       float64   `json:"db_uptime_s,omitempty"`
	Error           string    `json:"error,omitempty"`
}

func main() {
	res := run()
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(res)
	if res.Error != "" {
		os.Exit(1)
	}
}

func run() result {
	res := result{Timestamp: time.Now().UTC()}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		res.Error = "DATABASE_URL is not set"
		return res
	}

	timeout := defaultTimeout
	if raw := os.Getenv("DBWAKE_TIMEOUT"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			res.Error = fmt.Sprintf("DBWAKE_TIMEOUT: %v", err)
			return res
		}
		timeout = parsed
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		res.Error = fmt.Sprintf("connect: %v", err)
		res.TotalMs = millis(time.Since(start))
		return res
	}
	defer conn.Close(context.Background())
	res.ConnectMs = millis(time.Since(start))

	res.Select1Ms, err = timeSelect1(ctx, conn)
	if err != nil {
		res.Error = fmt.Sprintf("select 1: %v", err)
		res.TotalMs = millis(time.Since(start))
		return res
	}
	res.Select1WarmMs, err = timeSelect1(ctx, conn)
	if err != nil {
		res.Error = fmt.Sprintf("select 1 (warm): %v", err)
		res.TotalMs = millis(time.Since(start))
		return res
	}

	var version string
	var postmasterStart time.Time
	var uptime float64
	err = conn.QueryRow(ctx,
		"select version(), pg_postmaster_start_time(), extract(epoch from now() - pg_postmaster_start_time())",
	).Scan(&version, &postmasterStart, &uptime)
	if err != nil {
		res.ServerVersion = fmt.Sprintf("unavailable: %v", err)
	} else {
		res.ServerVersion = version
		res.PostmasterStart = postmasterStart.UTC().Format(time.RFC3339)
		res.DBUptimeS = uptime
	}

	res.TotalMs = millis(time.Since(start))
	return res
}

func timeSelect1(ctx context.Context, conn *pgx.Conn) (float64, error) {
	start := time.Now()
	var one int
	if err := conn.QueryRow(ctx, "select 1").Scan(&one); err != nil {
		return 0, err
	}
	if one != 1 {
		return 0, fmt.Errorf("unexpected result %d", one)
	}
	return millis(time.Since(start)), nil
}

func millis(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
