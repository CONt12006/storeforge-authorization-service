package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/storeforge/authorization-service/internal/cache"
	sessionclient "github.com/storeforge/authorization-service/internal/clients/session"
	"github.com/storeforge/authorization-service/internal/config"
	"github.com/storeforge/authorization-service/internal/repository/postgres"
	"github.com/storeforge/authorization-service/internal/service"
	"github.com/storeforge/authorization-service/internal/transport/httpserver"
)

func main() {
	ctx := context.Background()
	cfg, err := config.NewLoader().Load()
	if err != nil {
		log.Fatal(err)
	}
	repository, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()
	accessCache := cache.NewRedisAccessCache(cfg.RedisAddress, cfg.RedisPassword, cfg.RedisDatabase)
	defer accessCache.Close()
	sessionValidator := sessionclient.NewHTTPClient(cfg.SessionServiceURL, cfg.SessionServiceAPIKey, cfg.HTTPClientTimeout)
	authorizationService := service.NewAuthorizationService(
		repository,
		accessCache,
		sessionValidator,
		cfg.SessionValidationEnabled,
		cfg.PermissionsCacheTTL,
	)
	server := httpserver.New(cfg.HTTPAddress, cfg.InternalAPIKey, authorizationService)
	stopContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	<-stopContext.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Print(err)
	}
}
