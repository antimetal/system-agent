// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	addr = ":8080"
)

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
}

func main() {
	slog.Info("Starting OpAMP server...")

	server := NewServer(DefaultAgentRegistry)
	if err := server.Start(addr); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGKILL, syscall.SIGTERM)
	<-interrupt

	slog.Info("Received shutdown signal, stopping server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		slog.Error("Failed to stop server", "error", err)
		os.Exit(1)
	}
}
