package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhdph/taskmanager/internal/config"

	"github.com/mhdph/taskmanager/internal/hosted"

	"github.com/mhdph/taskmanager/internal/auth"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	hostedHTTPClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	hostedClient := hosted.NewClient(
		cfg.Hosted.GraphQLEndpoint,
		hostedHTTPClient,
	)

	identityCache := auth.NewIdentityCache(
		3 * time.Minute,
	)

	identityResolver := auth.NewResolver(
		hostedClient,
		identityCache,
	)

	authMiddleware := auth.NewMiddleware(
		identityResolver,
	)
	ctx := context.Background()

	poolConfig, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		log.Fatal(err)
	}

	poolConfig.MaxConns = 10
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.MaxConnIdleTime = 5 * time.Minute

	db, err := pgxpool.NewWithConfig(ctx, poolConfig)

	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPing()

	err = db.Ping(pingCtx)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("database connected")

	mux := http.NewServeMux()

	mux.HandleFunc("/", handleHome)
	mux.Handle(
		"/me",
		authMiddleware.Authenticate(
			http.HandlerFunc(handleMe),
		),
	)

	server := &http.Server{
		Addr:         cfg.Server.Address(),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf(
			"server started environment=%s address=%s",
			cfg.Environment,
			cfg.Server.Address(),
		)

		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	shutdown := make(chan os.Signal, 1)

	signal.Notify(
		shutdown,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	<-shutdown

	log.Println("shutting down server...")

	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancelShutdown()

	err = server.Shutdown(shutdownCtx)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("server stopped")
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]string{
		"message": "Task Manager API",
	}

	w.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}
func handleMe(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusInternalServerError,
		)
		return
	}

	response := map[string]string{
		"userId":    identity.UserID,
		"tenantId":  identity.TenantID,
		"email":     identity.Email,
		"firstName": identity.FirstName,
		"lastName":  identity.LastName,
		"nupRole":   string(identity.Role),
	}

	w.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Printf("encode me response: %v", err)
	}
}
