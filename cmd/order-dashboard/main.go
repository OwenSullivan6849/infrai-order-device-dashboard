package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/infrai-order-dashboard/dashboard"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	baseURL := os.Getenv("INFRAI_BASE_URL")
	if baseURL == "" {
		baseURL = dashboard.DefaultBaseURL
	}
	validatedURL, err := dashboard.ValidateBaseURL(baseURL)
	if err != nil {
		log.Fatal(err)
	}

	client := dashboard.NewInfraiClient(key, validatedURL, nil)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := client.CreateChannel(ctx, "commerce-device-status"); err != nil {
		log.Fatalf("create realtime channel: %v", err)
	}

	service := dashboard.NewService(dashboard.NewStore(), client, client.MetricsHandler)
	server := &http.Server{Addr: ":8080", Handler: service.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("order dashboard listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
