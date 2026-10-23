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
	"github.com/mhdph/taskmanager/internal/automation"
	"github.com/mhdph/taskmanager/internal/chat"
	"github.com/mhdph/taskmanager/internal/comment"
	"github.com/mhdph/taskmanager/internal/config"
	"github.com/mhdph/taskmanager/internal/dashboard"
	database "github.com/mhdph/taskmanager/internal/database/generated"
	"github.com/mhdph/taskmanager/internal/favorite"
	"github.com/mhdph/taskmanager/internal/hosted"
	"github.com/mhdph/taskmanager/internal/httpx"
	"github.com/mhdph/taskmanager/internal/member"
	"github.com/mhdph/taskmanager/internal/notification"
	"github.com/mhdph/taskmanager/internal/project"
	"github.com/mhdph/taskmanager/internal/search"
	"github.com/mhdph/taskmanager/internal/sprint"
	"github.com/mhdph/taskmanager/internal/storage"
	"github.com/mhdph/taskmanager/internal/storagegc"
	"github.com/mhdph/taskmanager/internal/task"
	"github.com/mhdph/taskmanager/internal/team"
	"github.com/mhdph/taskmanager/internal/view"
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

	if cfg.Storage.Endpoint == "" {
		log.Printf(
			"storage disabled: MINIO_ENDPOINT not set (attachment endpoints will fail)",
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

	// Attachment objects are removed on the happy path, but a crash, a
	// failed cleanup or a cascading database delete can leave objects
	// behind. The reconciler removes the objects that no live attachment
	// row references any more.
	if storageClient.Enabled() {
		reconciler := storagegc.NewReconciler(
			storageClient,
			storagegc.NewSQLReferences(queries),
		)

		go reconciler.Run(
			ctx,
			time.Hour,
			storagegc.Options{
				GracePeriod: cfg.Storage.ReconcileGracePeriod,
			},
		)
	}

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
		cfg.Hosted.DevMode,
	)

	// -------------------------------------------------------------------------
	// Member module
	// -------------------------------------------------------------------------

	memberRepository := member.NewRepository(
		pool,
		queries,
	)

	memberService := member.NewService(
		memberRepository,
	)

	memberHandler := member.NewHandler(
		memberService,
	)

	// -------------------------------------------------------------------------
	// Teams module
	// -------------------------------------------------------------------------

	teamHandler := team.NewHandler(
		team.NewService(
			team.NewRepository(
				pool,
				queries,
			),
			memberRepository,
		),
	)

	// Keep the tenant member directory in sync with hosted identities. Runs
	// only on cache misses (fresh hosted resolution), not on every request.
	identityResolver.SetSyncFunc(
		func(ctx context.Context, identity auth.Identity) {
			memberService.Sync(ctx, identity)
		},
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
	// Chat module
	// -------------------------------------------------------------------------

	chatBroker := chat.NewPostgresBroker(pool)

	chatRepository := chat.NewRepository(
		pool,
		queries,
		chatBroker,
	)

	chatHub := chat.NewHub(chatRepository, chatBroker)

	if err := chatBroker.Start(
		ctx,
		chatHub.HandleEvent,
	); err != nil {
		log.Fatalf("start chat broker: %v", err)
	}

	// Presence leases expire when an instance dies; every instance sweeps
	// so a crash is reflected without a central coordinator.
	go chatHub.RunPresenceCleanup(ctx)

	// Chat is high volume: without a configured notification service,
	// enqueueing would only log delivery failures for every message.
	var chatNotifications chat.NotificationSender
	if cfg.Notification.BaseURL != "" {
		chatNotifications = notificationDispatcher
	}

	chatService := chat.NewService(
		chatRepository,
		projectRepository,
		projectAuthorizer,
		chatBroker,
		storageClient,
		cfg.Storage.MaxUploadSize,
		chatRepository,
		chatNotifications,
	)

	// Pending uploads that were never sent are swept periodically so
	// abandoned uploads do not keep objects in the bucket forever.
	go chatService.RunAttachmentCleanup(
		ctx,
		time.Hour,
		24*time.Hour,
	)

	// Unused stream tickets expire after a minute; sweeping keeps the
	// table from growing with abandoned handshakes.
	go chatService.RunStreamTokenCleanup(
		ctx,
		15*time.Minute,
	)

	chatHandler := chat.NewHandler(
		chatService,
		chatHub,
		cfg.Chat.AllowedOrigins,
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

	// Activity module (read access to the audit feed)
	auditService := audit.NewService(
		auditRepository,
		projectAuthorizer,
	)

	auditHandler := audit.NewHandler(
		auditService,
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

	reminderCtx, stopReminders := context.WithCancel(ctx)
	defer stopReminders()
	remindersDone := make(chan struct{})
	go func() {
		defer close(remindersDone)
		if cfg.Notification.BaseURL != "" {
			taskService.RunReminders(reminderCtx, notificationClient)
		}
	}()

	// -------------------------------------------------------------------------
	// Automation module
	// -------------------------------------------------------------------------

	automationRepository := automation.NewRepository(
		pool,
		queries,
	)

	automationService := automation.NewService(
		automationRepository,
		taskRepository,
		projectRepository,
		projectAuthorizer,
		notificationDispatcher,
		cfg.Automation.MaxRulesPerProject,
	)

	// The two services need each other to apply rule actions while still
	// publishing the task events rules react to.
	automationService.SetTaskUpdater(taskService)
	taskService.SetAutomationNotifier(automationService)

	automationHandler := automation.NewHandler(
		automationService,
	)

	// Due-date rules are swept in the background; task events are
	// evaluated by the same worker pool.
	go automationService.Run(
		ctx,
		cfg.Automation.SweepInterval,
	)

	// -------------------------------------------------------------------------
	// Sprint module
	// -------------------------------------------------------------------------

	sprintRepository := sprint.NewRepository(
		pool,
		queries,
	)

	sprintService := sprint.NewService(
		sprintRepository,
		projectRepository,
		projectAuthorizer,
		auditRepository,
	)

	sprintHandler := sprint.NewHandler(
		sprintService,
	)

	// -------------------------------------------------------------------------
	// Favorites module
	// -------------------------------------------------------------------------

	favoriteRepository := favorite.NewRepository(
		pool,
		queries,
	)

	favoriteService := favorite.NewService(
		favoriteRepository,
		taskRepository,
		projectRepository,
		projectAuthorizer,
	)

	favoriteHandler := favorite.NewHandler(
		favoriteService,
	)

	// -------------------------------------------------------------------------
	// Saved views module
	// -------------------------------------------------------------------------

	viewHandler := view.NewHandler(
		view.NewService(
			view.NewRepository(queries),
			projectAuthorizer,
		),
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
	router.Use(httpx.SecurityHeaders)

	// Public routes
	router.Get("/", handleHome)
	router.Get("/health", readinessHandler(pool.Ping, storageClient.CheckHealth))
	router.Get("/live", handleHealth)
	router.Post("/auth/bridge", auth.BridgeHandler(hostedClient))

	// Authenticated user
	router.With(
		authMiddleware.Authenticate,
	).Get(
		"/me",
		handleMe,
	)

	// One-time ticket for browser WebSocket handshakes (browsers cannot
	// set custom headers on the upgrade request).
	router.With(
		authMiddleware.Authenticate,
	).Post(
		"/chat/stream-token",
		chatHandler.IssueStreamToken,
	)

	// Chat WebSocket stream (all conversations of the authenticated user).
	// The route accepts either the normal headers or a one-time token.
	router.With(
		streamAuthMiddleware(
			authMiddleware.Authenticate,
		),
	).Get(
		"/chat/stream",
		chatHandler.Stream,
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

	// Workspace tasks (tenant-wide listing + bulk operations)
	router.Route(
		"/tasks",
		func(r chi.Router) {
			r.Use(
				authMiddleware.Authenticate,
			)

			r.Get(
				"/",
				taskHandler.WorkspaceList,
			)

			r.Post(
				"/bulk",
				taskHandler.Bulk,
			)
		},
	)

	// My work (cross-project task list for the authenticated user)
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

	// Activity (tenant-wide audit feed, scoped by project membership)
	router.Route(
		"/activity",
		func(r chi.Router) {
			r.Use(
				authMiddleware.Authenticate,
			)

			r.Get(
				"/",
				auditHandler.Activity,
			)
		},
	)

	// Favorites (personal bookmarks over tasks and projects)
	router.Route(
		"/favorites",
		func(r chi.Router) {
			r.Use(
				authMiddleware.Authenticate,
			)

			r.Get(
				"/",
				favoriteHandler.Get,
			)

			r.Put(
				"/tasks/{taskID}",
				favoriteHandler.AddTask,
			)

			r.Delete(
				"/tasks/{taskID}",
				favoriteHandler.RemoveTask,
			)

			r.Put(
				"/projects/{projectID}",
				favoriteHandler.AddProject,
			)

			r.Delete(
				"/projects/{projectID}",
				favoriteHandler.RemoveProject,
			)
		},
	)

	// Labels (workspace label directory)
	router.Route(
		"/labels",
		func(r chi.Router) {
			r.Use(
				authMiddleware.Authenticate,
			)

			r.Get(
				"/",
				taskHandler.ListLabels,
			)

			r.Post(
				"/",
				taskHandler.CreateLabel,
			)

			r.Patch(
				"/{labelID}",
				taskHandler.UpdateLabel,
			)

			r.Delete(
				"/{labelID}",
				taskHandler.DeleteLabel,
			)
		},
	)

	// Members (workspace directory)
	router.With(authMiddleware.Authenticate).Get("/workload", memberHandler.Workload)
	router.Route(
		"/members",
		func(r chi.Router) {
			r.Use(
				authMiddleware.Authenticate,
			)
			r.Get(
				"/",
				memberHandler.List,
			)

			r.Get(
				"/{userID}",
				memberHandler.Get,
			)

			r.Get("/{userID}/capacity", memberHandler.GetCapacity)
			r.Put("/{userID}/capacity", memberHandler.SetCapacity)
			r.Delete("/{userID}/capacity", memberHandler.ClearCapacity)
			r.Get("/{userID}/workload", memberHandler.Workload)

			r.Patch(
				"/{userID}",
				memberHandler.Update,
			)

			r.Delete(
				"/{userID}",
				memberHandler.Remove,
			)
		},
	)

	// Teams (workspace directory of groups)
	router.Route(
		"/teams",
		func(r chi.Router) {
			r.Use(
				authMiddleware.Authenticate,
			)

			r.Get(
				"/",
				teamHandler.List,
			)

			r.Post(
				"/",
				teamHandler.Create,
			)

			r.Route(
				"/{teamID}",
				func(r chi.Router) {
					r.Get(
						"/",
						teamHandler.Get,
					)

					r.Patch(
						"/",
						teamHandler.Update,
					)

					r.Delete(
						"/",
						teamHandler.Delete,
					)

					r.Get(
						"/members",
						teamHandler.ListMembers,
					)

					r.Put(
						"/members",
						teamHandler.SetMembers,
					)
				},
			)
		},
	)

	// Conversations (direct, group and project chat)
	router.Route(
		"/conversations",
		func(r chi.Router) {
			r.Use(
				authMiddleware.Authenticate,
			)

			r.Get(
				"/",
				chatHandler.List,
			)

			r.Post(
				"/",
				chatHandler.Create,
			)

			r.Route(
				"/{conversationID}",
				func(r chi.Router) {
					r.Get(
						"/",
						chatHandler.Get,
					)

					r.Get(
						"/presence",
						chatHandler.Presence,
					)

					r.Get(
						"/notification-preference",
						chatHandler.GetNotificationPreference,
					)

					r.Put(
						"/notification-preference",
						chatHandler.SetNotificationPreference,
					)

					r.Post(
						"/attachments",
						chatHandler.UploadAttachment,
					)

					r.Get(
						"/attachments/{attachmentID}/download",
						chatHandler.DownloadAttachment,
					)

					r.Delete(
						"/attachments/{attachmentID}",
						chatHandler.CancelAttachment,
					)

					r.Get(
						"/messages",
						chatHandler.ListMessages,
					)

					r.Post(
						"/messages",
						chatHandler.SendMessage,
					)

					r.Patch(
						"/messages/{messageID}",
						chatHandler.EditMessage,
					)

					r.Delete(
						"/messages/{messageID}",
						chatHandler.DeleteMessage,
					)

					r.Post(
						"/read",
						chatHandler.MarkRead,
					)

					r.Post(
						"/members",
						chatHandler.AddMember,
					)

					r.Delete(
						"/members/{userID}",
						chatHandler.RemoveMember,
					)
				},
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
			r.Get("/activity", auditHandler.ProjectActivity)
			r.Get("/chat", chatHandler.ProjectChat)

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

			r.Route("/sprints", func(r chi.Router) {
				r.Get("/", sprintHandler.List)
				r.Post("/", sprintHandler.Create)

				r.Get("/{sprintID}", sprintHandler.Get)
				r.Patch("/{sprintID}", sprintHandler.Update)
				r.Delete("/{sprintID}", sprintHandler.Delete)

				r.Post("/{sprintID}/start", sprintHandler.Start)
				r.Post("/{sprintID}/complete", sprintHandler.Complete)
				r.Get("/{sprintID}/report", sprintHandler.Report)
			})

			r.Route("/views", func(r chi.Router) {
				r.Get("/", viewHandler.List)
				r.Post("/", viewHandler.Create)
				r.Patch("/{viewID}", viewHandler.Update)
				r.Delete("/{viewID}", viewHandler.Delete)
			})

			r.Route("/automation-rules", func(r chi.Router) {
				r.Get("/", automationHandler.List)
				r.Post("/", automationHandler.Create)
				r.Patch("/{ruleID}", automationHandler.Update)
				r.Delete("/{ruleID}", automationHandler.Delete)
			})

			r.Get(
				"/dashboard",
				dashboardHandler.Get,
			)
			r.Post("/ai/summary", aiHandler.SummarizeProject)

			r.Route("/tasks", func(r chi.Router) {
				r.Post("/", taskHandler.Create)
				r.Get("/", taskHandler.List)
				r.Put("/reorder", taskHandler.Reorder)

				r.Route("/{taskID}", func(r chi.Router) {
					r.Get("/", taskHandler.Get)
					r.Patch("/", taskHandler.Update)
					r.Delete("/", taskHandler.Archive)
					r.Post("/restore", taskHandler.Restore)
					r.Post("/claim", taskHandler.Claim)

					r.Route("/deliveries", func(r chi.Router) {
						r.Get("/", taskHandler.ListDeliveries)
						r.Post("/", taskHandler.SubmitDelivery)
						r.Get("/{deliveryID}", taskHandler.GetDelivery)
						r.Post("/{deliveryID}/review", taskHandler.ReviewDelivery)
						r.Post("/{deliveryID}/withdraw", taskHandler.WithdrawDelivery)
					})

					// Sprint assignment has its own endpoint: the sprint
					// module owns the planning rules.
					r.Put("/sprint", sprintHandler.SetTaskSprint)

					r.Route("/labels", func(r chi.Router) {
						r.Get("/", taskHandler.GetTaskLabels)
						r.Put("/", taskHandler.SetTaskLabels)
					})

					r.Route("/time-entries", func(r chi.Router) {
						r.Get("/", taskHandler.ListTimeEntries)
						r.Post("/start", taskHandler.StartTimer)
						r.Post("/stop", taskHandler.StopTimer)
						r.Delete("/{entryID}", taskHandler.DeleteTimeEntry)
					})

					r.Route("/reminders", func(r chi.Router) {
						r.Get("/", taskHandler.ListReminders)
						r.Post("/", taskHandler.CreateReminder)
						r.Post("/{reminderID}/snooze", taskHandler.SnoozeReminder)
						r.Delete("/{reminderID}", taskHandler.CancelReminder)
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

	stopReminders()
	select {
	case <-remindersDone:
	case <-shutdownCtx.Done():
		log.Printf("stop reminder worker: %v", shutdownCtx.Err())
	}

	// http.Server.Shutdown does not close hijacked WebSocket connections,
	// and would wait for their handlers. Close them and release their
	// presence leases before draining the server.
	chatHub.Close(shutdownCtx)

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown server: %v", err)
	}

	if err := chatBroker.Close(); err != nil {
		log.Printf("stop chat broker: %v", err)
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

// streamAuthMiddleware lets the WebSocket route authenticate with a one-time
// query token instead of the usual headers. The handler consumes the token and
// establishes the identity itself; when no token is present, header
// authentication is unchanged.
func streamAuthMiddleware(
	authenticate func(http.Handler) http.Handler,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		headerAuth := authenticate(next)

		return http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("token") != "" {
					next.ServeHTTP(w, r)
					return
				}

				headerAuth.ServeHTTP(w, r)
			},
		)
	}
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
