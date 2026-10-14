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

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mhdph/taskmanager/internal/ai"
	"github.com/mhdph/taskmanager/internal/audit"
	"github.com/mhdph/taskmanager/internal/auth"
	"github.com/mhdph/taskmanager/internal/comment"
	"github.com/mhdph/taskmanager/internal/config"
	"github.com/mhdph/taskmanager/internal/dashboard"
	database "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/hosted"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/search"
	"github.com/mhdph/taskmanager/internal/storage"
	"github.com/mhdph/taskmanager/internal/task"
)

func main() {
	// -------------------------------------------------------------------------
	// Config
	// -------------------------------------------------------------------------

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// -------------------------------------------------------------------------
	// Application context
	// -------------------------------------------------------------------------

	ctx := context.Background()

	// -------------------------------------------------------------------------
	// PostgreSQL
	// -------------------------------------------------------------------------

	poolConfig, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		log.Fatalf("parse database config: %v", err)
	}

	poolConfig.MaxConns = 10
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		log.Fatalf("create database pool: %v", err)
	}
	defer pool.Close()

	pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPing()

	if err := pool.Ping(pingCtx); err != nil {
		log.Fatalf("connect database: %v", err)
	}

	log.Println("database connected")

	// -------------------------------------------------------------------------
	// Minio
	// -------------------------------------------------------------------------
	storageClient, err :=
		storage.NewClient(
			cfg.Storage.Endpoint,
			cfg.Storage.AccessKey,
			cfg.Storage.SecretKey,
			cfg.Storage.Bucket,
			cfg.Storage.UseSSL,
		)

	if err != nil {
		log.Fatalf(
			"create storage client: %v",
			err,
		)
	}

	if cfg.Storage.PublicURL != "" {
		if err := storageClient.SetPublicURL(cfg.Storage.PublicURL, cfg.Storage.AccessKey, cfg.Storage.SecretKey); err != nil {
			log.Fatalf("configure public storage URL: %v", err)
		}
	}
	storageCtx, cancelStorage := context.WithTimeout(ctx, 15*time.Second)
	defer cancelStorage()
	if err := storageClient.EnsureBucket(
		storageCtx,
	); err != nil {
		log.Fatalf(
			"initialize storage bucket: %v",
			err,
		)
	}

	// -------------------------------------------------------------------------
	// SQLC
	// -------------------------------------------------------------------------

	queries := database.New(pool)

	// -------------------------------------------------------------------------
	// Hosted
	// -------------------------------------------------------------------------

	hostedHTTPClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	hostedClient := hosted.NewClient(
		cfg.Hosted.GraphQLEndpoint,
		hostedHTTPClient,
	)

	notificationHTTPClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	notificationClient :=
		notification.NewClient(
			cfg.Notification.BaseURL,
			notificationHTTPClient,
		)
	notificationDispatcher := notification.NewDispatcher(notificationClient)

	// -------------------------------------------------------------------------
	// Authentication
	// -------------------------------------------------------------------------

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

	// -------------------------------------------------------------------------
	// Project module
	// -------------------------------------------------------------------------

	projectRepository := project.NewRepository(
		pool,
		queries,
	)

	projectAuthorizer := project.NewAuthorizer(
		projectRepository,
	)

	projectService := project.NewService(
		projectRepository,
		projectAuthorizer,
	)

	projectHandler := project.NewHandler(
		projectService,
	)

	// -------------------------------------------------------------------------
	// Task module
	// -------------------------------------------------------------------------

	taskRepository := task.NewRepository(
		pool,
		queries,
	)

	auditRepository :=
		audit.NewRepository(
			queries,
		)

	taskService := task.NewService(
		taskRepository,
		projectRepository,
		projectAuthorizer,
		notificationDispatcher,
		storageClient,
		auditRepository,
		cfg.Storage.MaxUploadSize,
	)

	taskHandler := task.NewHandler(
		taskService,
	)

	// -------------------------------------------------------------------------
	// Comment module
	// -------------------------------------------------------------------------

	commentRepository :=
		comment.NewRepository(
			pool,
			queries,
		)

	commentService :=
		comment.NewService(
			commentRepository,
			taskRepository,
			projectRepository,
			projectAuthorizer,
			notificationDispatcher,
		)

	commentHandler :=
		comment.NewHandler(
			commentService,
		)

	// -------------------------------------------------------------------------
	// Search module
	// -------------------------------------------------------------------------
	searchRepository :=
		search.NewRepository(
			queries,
		)

	searchService :=
		search.NewService(
			searchRepository,
		)

	searchHandler :=
		search.NewHandler(
			searchService,
		)

	// -------------------------------------------------------------------------
	// Dashboard module
	// -------------------------------------------------------------------------

	dashboardRepository :=
		dashboard.NewRepository(
			queries,
		)

	dashboardService :=
		dashboard.NewService(
			dashboardRepository,
			projectAuthorizer,
		)

	dashboardHandler :=
		dashboard.NewHandler(
			dashboardService,
		)

	// -------------------------------------------------------------------------
	// AI module
	// -------------------------------------------------------------------------

	var aiClient ai.Client = ai.UnavailableClient{}
	if cfg.AI.APIKey != "" {
		aiClient, err = ai.NewDeepSeekClient(cfg.AI.APIKey, cfg.AI.BaseURL, cfg.AI.Model, &http.Client{
			Timeout:       25 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		})
		if err != nil {
			log.Fatalf("configure AI provider: %v", err)
		}
	}
	aiService := ai.NewService(
		aiClient,
		taskRepository,
		commentRepository,
		projectRepository,
		projectAuthorizer,
	)

	aiHandler := ai.NewHandler(aiService)

	// -------------------------------------------------------------------------
	// Router
	// -------------------------------------------------------------------------

	router := chi.NewRouter()

	// Public routes
	router.Get("/", handleHome)
	router.Get("/health", readinessHandler(pool.Ping, storageClient.CheckHealth))
	router.Get("/live", handleHealth)

	// Authenticated user
	router.With(
		authMiddleware.Authenticate,
	).Get(
		"/me",
		handleMe,
	)

	router.Route(
		"/search",
		func(r chi.Router) {
			r.Use(
				authMiddleware.Authenticate,
			)

			r.Get(
				"/",
				searchHandler.Search,
			)
		},
	)

	router.Route(
		"/ai",
		func(r chi.Router) {
			r.Use(
				authMiddleware.Authenticate,
			)

			r.Post(
				"/tasks/from-text",
				aiHandler.TaskFromText,
			)
		},
	)

	// Projects
	router.Route("/projects", func(r chi.Router) {
		r.Use(authMiddleware.Authenticate)

		r.Post("/", projectHandler.Create)
		r.Get("/", projectHandler.List)

		r.Route("/{projectID}", func(r chi.Router) {
			r.Get("/", projectHandler.Get)
			r.Patch("/", projectHandler.Update)
			r.Delete("/", projectHandler.Archive)
			r.Post("/restore", projectHandler.Restore)

			r.Route("/members", func(r chi.Router) {
				r.Get("/", projectHandler.ListMembers)
				r.Post("/", projectHandler.AddMember)
				r.Patch("/{userID}", projectHandler.UpdateMemberRole)
				r.Delete("/{userID}", projectHandler.RemoveMember)
			})

			r.Route("/statuses", func(r chi.Router) {
				r.Get("/", projectHandler.ListStatuses)
				r.Post("/", projectHandler.CreateStatus)
				r.Put("/reorder", projectHandler.ReorderStatuses)
				r.Patch("/{statusID}", projectHandler.UpdateStatus)
				r.Delete("/{statusID}", projectHandler.DeleteStatus)
			})

			router.Route(
				"/my-work",
				func(r chi.Router) {
					r.Use(
						authMiddleware.Authenticate,
					)

					r.Get(
						"/",
						taskHandler.MyWork,
					)
				},
			)
			r.Get(
				"/dashboard",
				dashboardHandler.Get,
			)
			r.Post("/ai/summary", aiHandler.SummarizeProject)

			r.Route("/tasks", func(r chi.Router) {
				r.Post("/", taskHandler.Create)
				r.Get("/", taskHandler.List)

				r.Route("/{taskID}", func(r chi.Router) {
					r.Get("/", taskHandler.Get)
					r.Patch("/", taskHandler.Update)
					r.Delete("/", taskHandler.Archive)
					r.Post("/restore", taskHandler.Restore)
					r.Route("/time-entries", func(r chi.Router) {
						r.Get("/", taskHandler.ListTimeEntries)
						r.Post("/start", taskHandler.StartTimer)
						r.Post("/stop", taskHandler.StopTimer)
						r.Delete("/{entryID}", taskHandler.DeleteTimeEntry)
					})
					r.Route("/watchers", func(r chi.Router) {
						r.Get("/", taskHandler.ListWatchers)
						r.Post("/", taskHandler.AddWatcher)
						r.Delete("/{userID}", taskHandler.RemoveWatcher)
					})
					r.Route("/ai", func(r chi.Router) {
						r.Post("/summary", aiHandler.SummarizeTask)
						r.Post("/suggestions", aiHandler.SuggestTask)
					})
					r.Route(
						"/comments",
						func(r chi.Router) {
							r.Post(
								"/",
								commentHandler.Create,
							)

							r.Get(
								"/",
								commentHandler.List,
							)

							r.Delete(
								"/{commentID}",
								commentHandler.Delete,
							)
						},
					)

					r.Route(
						"/attachments",
						func(r chi.Router) {
							r.Get(
								"/",
								taskHandler.ListAttachments,
							)

							r.Post(
								"/",
								taskHandler.UploadAttachment,
							)

							r.Route(
								"/{attachmentID}",
								func(r chi.Router) {
									r.Get(
										"/download",
										taskHandler.DownloadAttachment,
									)

									r.Delete(
										"/",
										taskHandler.DeleteAttachment,
									)
								},
							)
						},
					)

					r.Route(
						"/dependencies",
						func(r chi.Router) {
							r.Get(
								"/",
								taskHandler.ListDependencies,
							)

							r.Post(
								"/",
								taskHandler.AddDependency,
							)

							r.Delete(
								"/{dependsOnTaskID}",
								taskHandler.RemoveDependency,
							)
						},
					)
				})
			})
		})
	})

	// -------------------------------------------------------------------------
	// HTTP Server
	// -------------------------------------------------------------------------

	server := &http.Server{
		Addr:              cfg.Server.Address(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// -------------------------------------------------------------------------
	// Start server
	// -------------------------------------------------------------------------

	go func() {
		log.Printf(
			"server started environment=%s address=%s",
			cfg.Environment,
			cfg.Server.Address(),
		)

		err := server.ListenAndServe()

		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	// -------------------------------------------------------------------------
	// Graceful shutdown
	// -------------------------------------------------------------------------

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

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown server: %v", err)
	}

	if err := notificationDispatcher.Close(shutdownCtx); err != nil {
		log.Printf("drain notification queue: %v", err)
	}

	log.Println("server stopped")
}

func handleHome(
	w http.ResponseWriter,
	r *http.Request,
) {
	writeJSON(
		w,
		http.StatusOK,
		map[string]string{
			"message": "Task Manager API",
		},
	)
}

func handleHealth(
	w http.ResponseWriter,
	r *http.Request,
) {
	writeJSON(
		w,
		http.StatusOK,
		map[string]string{
			"status": "ok",
		},
	)
}

func handleMe(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(
			w,
			"identity not found",
			http.StatusInternalServerError,
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		map[string]any{
			"userId":    identity.UserID,
			"tenantId":  identity.TenantID,
			"email":     identity.Email,
			"firstName": identity.FirstName,
			"lastName":  identity.LastName,
			"role":      identity.Role,
			"isSupport": identity.IsSupport,
		},
	)
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	data any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("encode json response: %v", err)
	}
}
