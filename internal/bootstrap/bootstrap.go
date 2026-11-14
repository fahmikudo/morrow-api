// Package bootstrap wires configuration, logging, the database, every module,
// and the router. It is the only package that knows the whole graph.
package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"morrow-api/internal/config"
	"morrow-api/internal/database"
	"morrow-api/internal/module/apitoken"
	"morrow-api/internal/module/area"
	"morrow-api/internal/module/assistant"
	"morrow-api/internal/module/attachment"
	"morrow-api/internal/module/auth"
	"morrow-api/internal/module/calendar"
	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/importer"
	"morrow-api/internal/module/inbox"
	"morrow-api/internal/module/note"
	"morrow-api/internal/module/profile"
	"morrow-api/internal/module/project"
	"morrow-api/internal/module/reminder"
	"morrow-api/internal/module/search"
	"morrow-api/internal/module/settings"
	"morrow-api/internal/module/snapshot"
	"morrow-api/internal/module/task"
	"morrow-api/internal/module/tasksync"
	"morrow-api/internal/module/tasksync/pepy"
	"morrow-api/internal/platform/google"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/objectstore"
	"morrow-api/internal/platform/secretbox"
)

// Deps is the state the router is assembled from. Tests pass a database and no
// client; production passes a shared 20-second client.
type Deps struct {
	DB         *gorm.DB
	HTTPClient *http.Client
	Calendar   *calendar.Service
	TaskSync   *tasksync.Service
}

// App is a running server: the handler plus what it was built from.
type App struct {
	Config     *config.Config
	Logger     *slog.Logger
	DB         *gorm.DB
	HTTPClient *http.Client
	Handler    http.Handler
	Reminders  *reminder.Scheduler
	Calendar   *calendar.Service
	TaskSync   *tasksync.Service
}

// New opens the database, builds the router, and returns the app.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	db, err := database.Open(ctx, cfg, log)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 20 * time.Second}
	calendarService := newCalendarService(cfg, log, db, client)
	taskSyncService := newTaskSyncService(cfg, log, db, client)

	scheduler := reminder.NewScheduler(
		db,
		log,
		reminder.NewRepository(db),
		task.NewRepository(db),
		event.NewRepository(db),
		habit.NewRepository(db),
		profile.NewRepository(db),
		reminder.NewPushSender(cfg, client),
		time.Duration(cfg.ReminderTickSeconds)*time.Second,
		cfg.AppTimezone,
	)

	return &App{
		Config:     cfg,
		Logger:     log,
		DB:         db,
		HTTPClient: client,
		Handler: NewRouter(cfg, log, Deps{
			DB:         db,
			HTTPClient: client,
			Calendar:   calendarService,
			TaskSync:   taskSyncService,
		}),
		Reminders: scheduler,
		Calendar:  calendarService,
		TaskSync:  taskSyncService,
	}, nil
}

func newAttachmentService(
	cfg *config.Config,
	log *slog.Logger,
	db *gorm.DB,
) *attachment.Service {
	store, err := objectstore.NewS3(context.Background(), cfg)
	if err != nil {
		log.Warn("attachments disabled: object storage unavailable", "error", err)
		store = nil
	}

	return attachment.NewService(
		db,
		log,
		attachment.NewRepository(db),
		store,
		time.Duration(cfg.AttachmentURLTTLSeconds)*time.Second,
	)
}

func newTaskSyncService(
	cfg *config.Config,
	log *slog.Logger,
	db *gorm.DB,
	client *http.Client,
) *tasksync.Service {
	box, err := secretbox.New(cfg.AppEncryptionKey)
	if err != nil {
		log.Warn("Pepy sync disabled: invalid APP_ENCRYPTION_KEY", "error", err)
		box = nil
	}

	repo := tasksync.NewRepository(db)
	provider := pepy.NewProvider(
		cfg, client, repo, profile.NewRepository(db), box, log,
	)

	return tasksync.NewService(
		db, log, repo, task.NewRepository(db),
		time.Duration(cfg.PepySyncSeconds)*time.Second,
		provider,
	)
}

func newCalendarService(
	cfg *config.Config,
	log *slog.Logger,
	db *gorm.DB,
	client *http.Client,
) *calendar.Service {
	box, err := secretbox.New(cfg.AppEncryptionKey)
	if err != nil {
		log.Warn("google calendar disabled: invalid APP_ENCRYPTION_KEY", "error", err)
		box = nil
	}

	return calendar.NewService(
		db,
		cfg,
		log,
		event.NewRepository(db),
		profile.NewRepository(db),
		settings.NewRepository(db),
		google.New(cfg, client),
		box,
	)
}

// Close releases the connection pool.
func (a *App) Close() error {
	sqlDB, err := a.DB.DB()
	if err != nil {
		return err
	}

	return sqlDB.Close()
}

// NewRouter assembles repositories, services, handlers, and the route tree.
func NewRouter(cfg *config.Config, log *slog.Logger, deps Deps) http.Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	client := deps.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	accounts := auth.NewRepository(deps.DB)
	profiles := profile.NewRepository(deps.DB)
	apiTokens := apitoken.NewRepository(deps.DB)
	areas := area.NewRepository(deps.DB)
	projects := project.NewRepository(deps.DB)
	tasks := task.NewRepository(deps.DB)
	counters := task.NewCounters(deps.DB)
	notes := note.NewRepository(deps.DB)
	events := event.NewRepository(deps.DB)
	habits := habit.NewRepository(deps.DB)
	inboxItems := inbox.NewRepository(deps.DB)
	settingsRepo := settings.NewRepository(deps.DB)
	importRepo := importer.NewRepository(deps.DB)
	reminderRepo := reminder.NewRepository(deps.DB)
	attachmentService := newAttachmentService(cfg, log, deps.DB)

	calendarService := deps.Calendar
	if calendarService == nil {
		calendarService = newCalendarService(cfg, log, deps.DB, client)
	}
	taskSyncService := deps.TaskSync
	if taskSyncService == nil {
		taskSyncService = newTaskSyncService(cfg, log, deps.DB, client)
	}

	authService := auth.NewService(deps.DB, cfg, log, accounts, profiles, settingsRepo, reminderRepo)
	apiTokenService := apitoken.NewService(deps.DB, log, apiTokens)
	profileService := profile.NewService(deps.DB, log, profiles, accounts)
	areaService := area.NewService(deps.DB, log, areas)
	projectService := project.NewService(deps.DB, log, projects, areas, attachmentService)
	taskService := task.NewService(
		deps.DB, log, tasks, projects, areas, counters, taskSyncService, attachmentService,
	)
	noteService := note.NewService(deps.DB, log, notes, projects, areas, attachmentService)
	eventService := event.NewService(deps.DB, log, events, projects, calendarService)
	habitService := habit.NewService(deps.DB, log, habits, projects, areas)
	inboxService := inbox.NewService(deps.DB, log, inboxItems)
	settingsService := settings.NewService(deps.DB, cfg, log, settingsRepo, profiles, client)
	pushSender := reminder.NewPushSender(cfg, client)
	reminderService := reminder.NewService(
		deps.DB, cfg, log, reminderRepo, profiles, pushSender,
	)
	importService := importer.NewService(deps.DB, log, importRepo, importer.Deps{
		Areas:    areas,
		Projects: projects,
		Inbox:    inboxItems,
		Tasks:    tasks,
		Notes:    notes,
		Events:   events,
		Habits:   habits,
		Counters: counters,
		Profiles: profiles,
	})

	searchService := search.NewService(log, tasks, notes, projects, inboxItems, areas, habits)
	snapshotService := snapshot.NewService(log, snapshot.Deps{
		Tasks:    taskService,
		Notes:    notes,
		Projects: projects,
		Events:   events,
		Areas:    areas,
		Inbox:    inboxItems,
		Habits:   habits,
		CheckIns: habits,
		Skips:    habits,
		Profiles: profiles,
		Counters: counters,
		Settings: settingsService,
	})
	assistantService := assistant.NewService(
		cfg, log, client,
		tasks, projects, notes, inboxItems,
		events, habits, areas,
		taskService,
		settingsService,
	)

	authHandler := auth.NewHandler(authService)
	apiTokenHandler := apitoken.NewHandler(apiTokenService)
	profileHandler := profile.NewHandler(profileService)
	areaHandler := area.NewHandler(areaService)
	projectHandler := project.NewHandler(projectService)
	taskHandler := task.NewHandler(taskService)
	noteHandler := note.NewHandler(noteService)
	eventHandler := event.NewHandler(eventService)
	habitHandler := habit.NewHandler(habitService)
	inboxHandler := inbox.NewHandler(inboxService)
	settingsHandler := settings.NewHandler(settingsService)
	reminderHandler := reminder.NewHandler(reminderService)
	attachmentHandler := attachment.NewHandler(attachmentService)
	calendarHandler := calendar.NewHandler(calendarService)
	taskSyncHandler := tasksync.NewHandler(taskSyncService, settingsRepo)
	importHandler := importer.NewHandler(importService)
	searchHandler := search.NewHandler(searchService)
	snapshotHandler := snapshot.NewHandler(snapshotService)
	assistantHandler := assistant.NewHandler(assistantService)

	return httpx.NewRouter(httpx.RouterOptions{
		CORSOrigins:   cfg.CORSOrigins,
		Logger:        log,
		BodyLimit:     10 << 20,
		Authenticator: authService,
		Public: func(router chi.Router) {
			authHandler.RegisterPublic(router)
			calendarHandler.RegisterPublic(router)
		},
		Protected: func(router chi.Router) {
			authHandler.Register(router)
			apiTokenHandler.Register(router)
			profileHandler.Register(router)
			inboxHandler.Register(router)
			taskHandler.Register(router)
			projectHandler.Register(router)
			noteHandler.Register(router)
			eventHandler.Register(router)
			areaHandler.Register(router)
			habitHandler.Register(router)
			settingsHandler.Register(router)
			reminderHandler.Register(router)
			attachmentHandler.Register(router)
			calendarHandler.Register(router)
			taskSyncHandler.Register(router)
			snapshotHandler.Register(router)
			importHandler.Register(router)
			searchHandler.Register(router)
			assistantHandler.Register(router)
		},
	})
}

// IsServerClosed reports the sentinel a graceful shutdown returns.
func IsServerClosed(err error) bool {
	return errors.Is(err, http.ErrServerClosed)
}
