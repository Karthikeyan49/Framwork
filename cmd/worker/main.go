package main

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/platform/app"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.Main(ctx, "framework-worker")
	stop()
	os.Exit(code)
}
