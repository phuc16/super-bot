package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/config"
	"github.com/phuc16/VCG-super-bot/internal/discord"
	"github.com/phuc16/VCG-super-bot/internal/discord/commands"
	"github.com/phuc16/VCG-super-bot/internal/discord/events"
	"github.com/phuc16/VCG-super-bot/internal/gpt"
	"github.com/phuc16/VCG-super-bot/internal/scheduler"
	"github.com/phuc16/VCG-super-bot/internal/services/activity"
	aitopicService "github.com/phuc16/VCG-super-bot/internal/services/aitopic"
	configService "github.com/phuc16/VCG-super-bot/internal/services/config"
	"github.com/phuc16/VCG-super-bot/internal/services/customcmd"
	"github.com/phuc16/VCG-super-bot/internal/services/export"
	"github.com/phuc16/VCG-super-bot/internal/services/reporting"
	"github.com/phuc16/VCG-super-bot/internal/services/roles"
	"github.com/phuc16/VCG-super-bot/internal/storage"
	"github.com/phuc16/VCG-super-bot/internal/storage/migrations"
	"github.com/phuc16/VCG-super-bot/pkg/logger"
)

func main() {
	// Handle command line arguments
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			runMigrations()
			return
		case "version":
			fmt.Println("Discord Super Bot v1.0.0")
			return
		default:
			fmt.Printf("Unknown command: %s\n", os.Args[1])
			fmt.Println("Available commands: migrate, version")
			os.Exit(1)
		}
	}

	// Run the bot
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	logger.Init(cfg.LogLevel)
	log.Info().Msg("Discord Super Bot starting...")
	log.Info().
		Str("environment", cfg.AppEnv).
		Str("log_level", cfg.LogLevel).
		Bool("debug_mode", cfg.DebugMode).
		Bool("mock_gpt", cfg.MockGPTAPI).
		Msg("Configuration loaded and validated successfully")

	// Ensure rate limiters are cleaned up on exit
	defer commands.CloseAllRateLimiters()

	// Connect to database
	log.Info().Msg("Connecting to database...")
	if err := storage.ConnectWithMigrations(cfg.GetDSN(), cfg.DebugMode); err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}
	defer storage.Close()

	// Log initial connection pool stats
	storage.LogConnectionStats()

	// Initialize Discord client
	log.Info().Msg("Initializing Discord client...")
	discordClient, err := initializeDiscord(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize Discord: %w", err)
	}
	defer discordClient.Close()

	// Start health check server (after Discord is initialized)
	_ = startHealthServer(cfg.Port, storage.GetDB(), discordClient, cfg)

	// Initialize GPT client and queue
	log.Info().Msg("Initializing GPT client...")
	gptClient, gptQueue := initializeGPT(cfg)
	defer gptQueue.Shutdown()

	// Initialize services
	log.Info().Msg("Initializing services...")
	exportService, activityTracker, roleAnalyzer, _, reportGen, reportPoster, inactivityDetector, configSvc, customcmdSvc, aiTopicSvc, taskScheduler := initializeServices(storage.GetDB(), discordClient, gptClient, gptQueue, cfg)

	// Start activity tracker
	log.Info().Msg("Starting activity tracker...")
	activityTracker.Start()

	// Register event handlers
	log.Info().Msg("Registering event handlers...")
	registerEventHandlers(discordClient, activityTracker, customcmdSvc)

	// Start scheduler
	log.Info().Msg("Starting task scheduler...")
	if err := taskScheduler.Start(); err != nil {
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	// Register commands
	log.Info().Msg("Registering Discord commands...")
	if err := registerCommands(discordClient, exportService, roleAnalyzer, reportGen, reportPoster, inactivityDetector, configSvc, customcmdSvc, aiTopicSvc, cfg); err != nil {
		return fmt.Errorf("failed to register commands: %w", err)
	}

	log.Info().Msg("✅ Bot is ready!")

	// Wait for interrupt signal
	// Start periodic metrics logging
	metricsTicker := time.NewTicker(5 * time.Minute)
	defer metricsTicker.Stop()
	go func() {
		for range metricsTicker.C {
			logger.LogMetricsSummary()
			storage.LogConnectionStats()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info().Msg("Shutting down gracefully...")

	// Log final metrics before shutdown
	logger.LogMetricsSummary()
	storage.LogConnectionStats()

	// Shutdown activity tracker
	if err := activityTracker.Shutdown(); err != nil {
		log.Error().Err(err).Msg("Failed to shutdown activity tracker")
	}

	// Shutdown scheduler
	taskScheduler.Stop()

	return nil
}

func runMigrations() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bot migrate [up|down|version]")
		os.Exit(1)
	}

	// Load config for database connection
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger.Init(cfg.LogLevel)

	// Connect to database using database/sql for migrations
	db, err := sql.Open("mysql", cfg.GetDSN())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()

	migrator := migrations.NewMigrator(db)

	switch os.Args[2] {
	case "up":
		if err := migrator.Up(); err != nil {
			log.Fatal().Err(err).Msg("Migration failed")
		}
	case "down":
		if err := migrator.Down(); err != nil {
			log.Fatal().Err(err).Msg("Rollback failed")
		}
	case "version":
		// TODO: Implement version check
		fmt.Println("Current version: Not implemented yet")
	default:
		fmt.Printf("Unknown migration command: %s\n", os.Args[2])
		fmt.Println("Available commands: up, down, version")
		os.Exit(1)
	}
}

func initializeDiscord(cfg *config.Config) (*discord.Client, error) {
	client, err := discord.NewClient(cfg.DiscordToken, cfg.DiscordGuildID)
	if err != nil {
		return nil, err
	}

	if err := client.Connect(); err != nil {
		return nil, err
	}

	return client, nil
}

func initializeGPT(cfg *config.Config) (*gpt.Client, *gpt.Queue) {
	// Initialize cost tracker
	costTracker := gpt.NewCostTracker(cfg.GPTDailyBudgetUSD)

	// Initialize GPT client
	gptClient := gpt.NewClient(
		cfg.OpenAIAPIKey,
		cfg.GPTModel,
		cfg.GPTMaxTokens,
		cfg.MockGPTAPI,
		costTracker,
	)

	// Initialize summarizer
	summarizer := gpt.NewSummarizer(gptClient)

	// Initialize queue with configured workers
	queue := gpt.NewQueue(cfg.GPTMaxConcurrentRequests, summarizer)

	return gptClient, queue
}

func initializeServices(db *gorm.DB, discordClient *discord.Client, gptClient *gpt.Client, gptQueue *gpt.Queue, cfg *config.Config) (*export.Service, *activity.Tracker, *roles.Analyzer, *roles.Assigner, *reporting.Generator, *reporting.Poster, *activity.InactivityDetector, *configService.Service, *customcmd.Service, *aitopicService.Service, *scheduler.Scheduler) {
	// Export service
	exportService := export.NewService(db, discordClient.Session, gptQueue)

	// Activity tracker with 1000 event buffer
	activityTracker := activity.NewTracker(db, 1000)

	// Role services
	roleAnalyzer := roles.NewAnalyzer(db)
	roleAssigner := roles.NewAssigner(db, discordClient.Session)

	// Reporting services
	reportGen := reporting.NewGenerator(db)
	reportPoster := reporting.NewPoster(db, discordClient.Session)

	// Inactivity detector
	inactivityDetector := activity.NewInactivityDetector(db)

	// Config service
	configSvc := configService.NewService(db)

	// Custom command service
	customcmdSvc := customcmd.NewService(db)

	// AI topic service
	aiTopicSvc := aitopicService.NewService(db)

	// Task scheduler (now uses config service and Discord session)
	taskScheduler := scheduler.NewScheduler(db, discordClient.Session, roleAnalyzer, roleAssigner, reportGen, reportPoster, configSvc, aiTopicSvc, gptClient, cfg.DiscordGuildID)

	return exportService, activityTracker, roleAnalyzer, roleAssigner, reportGen, reportPoster, inactivityDetector, configSvc, customcmdSvc, aiTopicSvc, taskScheduler
}

func registerEventHandlers(discordClient *discord.Client, tracker *activity.Tracker, customcmdSvc *customcmd.Service) {
	// Message event handler
	messageHandler := events.NewMessageHandler(tracker, customcmdSvc)
	messageHandler.Register(discordClient.Session)

	// Voice event handler
	voiceHandler := events.NewVoiceHandler(tracker)
	voiceHandler.Register(discordClient.Session)
}

func registerCommands(discordClient *discord.Client, exportService *export.Service, roleAnalyzer *roles.Analyzer, reportGen *reporting.Generator, reportPoster *reporting.Poster, inactivityDetector *activity.InactivityDetector, configSvc *configService.Service, customcmdSvc *customcmd.Service, aiTopicSvc *aitopicService.Service, cfg *config.Config) error {
	// Create rate limiter for exports
	rateLimiter := export.NewRateLimiter(cfg.RateLimitExportPerHour)

	// Create command handlers
	exportCmd := commands.NewExportCommand(exportService, rateLimiter, cfg.Timezone)
	activityCmd := commands.NewActivityCommand(roleAnalyzer)
	leaderboardCmd := commands.NewLeaderboardCommand(roleAnalyzer)
	reportCmd := commands.NewReportCommand(reportGen, reportPoster, configSvc)
	inactiveCmd := commands.NewInactiveCommand(inactivityDetector)
	configCmd := commands.NewConfigCommand(configSvc)
	helpCmd := commands.NewHelpCommand()
	addcomCmd := commands.NewAddcomCommand(customcmdSvc)
	delcomCmd := commands.NewDelcomCommand(customcmdSvc)
	listcomCmd := commands.NewListcomCommand(customcmdSvc)
	addtopicCmd := commands.NewAddtopicCommand(aiTopicSvc)
	deltopicCmd := commands.NewDeltopicCommand(aiTopicSvc)
	listtopicCmd := commands.NewListtopicCommand(aiTopicSvc)

	// Register command definitions
	if err := discordClient.RegisterCommand(exportCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /export command: %w", err)
	}
	if err := discordClient.RegisterCommand(activityCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /activity command: %w", err)
	}
	if err := discordClient.RegisterCommand(leaderboardCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /leaderboard command: %w", err)
	}
	if err := discordClient.RegisterCommand(reportCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /report command: %w", err)
	}
	if err := discordClient.RegisterCommand(inactiveCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /inactive command: %w", err)
	}
	if err := discordClient.RegisterCommand(configCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /config command: %w", err)
	}
	if err := discordClient.RegisterCommand(helpCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /help command: %w", err)
	}
	if err := discordClient.RegisterCommand(addcomCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /addcom command: %w", err)
	}
	if err := discordClient.RegisterCommand(delcomCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /delcom command: %w", err)
	}
	if err := discordClient.RegisterCommand(listcomCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /listcom command: %w", err)
	}
	if err := discordClient.RegisterCommand(addtopicCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /addtopic command: %w", err)
	}
	if err := discordClient.RegisterCommand(deltopicCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /deltopic command: %w", err)
	}
	if err := discordClient.RegisterCommand(listtopicCmd.Definition()); err != nil {
		return fmt.Errorf("failed to register /listtopic command: %w", err)
	}

	log.Info().Msg("Registered 13 Discord commands: /export, /activity, /leaderboard, /report, /inactive, /config, /help, /addcom, /delcom, /listcom, /addtopic, /deltopic, /listtopic")

	// Create middleware-wrapped command handlers with rate limiting
	exportHandler := commands.ChainMiddleware(
		exportCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithUserRateLimit("export", commands.GlobalRateLimiters.Export, h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("export", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("export", h)
		},
	)

	activityHandler := commands.ChainMiddleware(
		activityCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithUserRateLimit("activity", commands.GlobalRateLimiters.Activity, h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("activity", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("activity", h)
		},
	)

	leaderboardHandler := commands.ChainMiddleware(
		leaderboardCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("leaderboard", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("leaderboard", h)
		},
	)

	reportHandler := commands.ChainMiddleware(
		reportCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithUserRateLimit("report", commands.GlobalRateLimiters.Report, h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("report", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("report", h)
		},
	)

	inactiveHandler := commands.ChainMiddleware(
		inactiveCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithUserRateLimit("inactive", commands.GlobalRateLimiters.Inactive, h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("inactive", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("inactive", h)
		},
	)

	configHandler := commands.ChainMiddleware(
		configCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithUserRateLimit("config", commands.GlobalRateLimiters.Config, h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("config", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("config", h)
		},
	)

	helpHandler := commands.ChainMiddleware(
		helpCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithUserRateLimit("help", commands.GlobalRateLimiters.Help, h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("help", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("help", h)
		},
	)

	addcomHandler := commands.ChainMiddleware(
		addcomCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("addcom", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("addcom", h)
		},
	)

	delcomHandler := commands.ChainMiddleware(
		delcomCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("delcom", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("delcom", h)
		},
	)

	listcomHandler := commands.ChainMiddleware(
		listcomCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("listcom", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("listcom", h)
		},
	)

	addtopicHandler := commands.ChainMiddleware(
		addtopicCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("addtopic", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("addtopic", h)
		},
	)

	deltopicHandler := commands.ChainMiddleware(
		deltopicCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("deltopic", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("deltopic", h)
		},
	)

	listtopicHandler := commands.ChainMiddleware(
		listtopicCmd.Handle,
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithRecovery("listtopic", h)
		},
		func(h commands.CommandHandler) commands.CommandHandler {
			return commands.WithLogging("listtopic", h)
		},
	)

	// Add interaction handler with middleware
	discordClient.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		// Only handle application commands
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}

		// Route to appropriate command handler with middleware
		switch i.ApplicationCommandData().Name {
		case "export":
			exportHandler(s, i)
		case "activity":
			activityHandler(s, i)
		case "leaderboard":
			leaderboardHandler(s, i)
		case "report":
			reportHandler(s, i)
		case "inactive":
			inactiveHandler(s, i)
		case "config":
			configHandler(s, i)
		case "help":
			helpHandler(s, i)
		case "addcom":
			addcomHandler(s, i)
		case "delcom":
			delcomHandler(s, i)
		case "listcom":
			listcomHandler(s, i)
		case "addtopic":
			addtopicHandler(s, i)
		case "deltopic":
			deltopicHandler(s, i)
		case "listtopic":
			listtopicHandler(s, i)
		default:
			log.Warn().Str("command", i.ApplicationCommandData().Name).Msg("Unknown command received")
		}
	})

	return nil
}

func startHealthServer(port string, db *gorm.DB, discordClient *discord.Client, cfg *config.Config) *gin.Engine {
	// Set Gin mode based on environment
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	// Health check endpoint - comprehensive health check
	router.GET("/health", func(c *gin.Context) {
		health := gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
			"checks":    gin.H{},
		}

		overallHealthy := true

		// Check database connection
		sqlDB, err := db.DB()
		if err != nil {
			health["checks"].(gin.H)["database"] = gin.H{"status": "unhealthy", "error": err.Error()}
			overallHealthy = false
		} else {
			err = sqlDB.Ping()
			if err != nil {
				health["checks"].(gin.H)["database"] = gin.H{"status": "unhealthy", "error": err.Error()}
				overallHealthy = false
			} else {
				health["checks"].(gin.H)["database"] = gin.H{"status": "healthy"}
			}
		}

		// Check Discord connection
		if discordClient == nil || discordClient.Session == nil {
			health["checks"].(gin.H)["discord"] = gin.H{"status": "unhealthy", "error": "discord client not initialized"}
			overallHealthy = false
		} else {
			// Check if session is open
			if discordClient.Session.DataReady {
				health["checks"].(gin.H)["discord"] = gin.H{"status": "healthy", "connected": true}
			} else {
				health["checks"].(gin.H)["discord"] = gin.H{"status": "degraded", "connected": false}
				// Don't mark as unhealthy if Discord is just not ready yet
			}
		}

		// GPT API check - just check if mock mode is enabled
		if cfg.MockGPTAPI {
			health["checks"].(gin.H)["gpt"] = gin.H{"status": "healthy", "mode": "mock"}
		} else {
			health["checks"].(gin.H)["gpt"] = gin.H{"status": "healthy", "mode": "live"}
		}

		if !overallHealthy {
			health["status"] = "unhealthy"
			c.JSON(503, health)
		} else {
			c.JSON(200, health)
		}
	})

	// Readiness check endpoint - simpler check for k8s/docker
	router.GET("/ready", func(c *gin.Context) {
		ready := true
		checks := gin.H{}

		// Check database
		sqlDB, err := db.DB()
		if err != nil || sqlDB.Ping() != nil {
			ready = false
			checks["database"] = "not ready"
		} else {
			checks["database"] = "ready"
		}

		// Check Discord
		if discordClient != nil && discordClient.Session != nil && discordClient.Session.DataReady {
			checks["discord"] = "ready"
		} else {
			checks["discord"] = "not ready"
			ready = false
		}

		if ready {
			c.JSON(200, gin.H{
				"status": "ready",
				"checks": checks,
			})
		} else {
			c.JSON(503, gin.H{
				"status": "not ready",
				"checks": checks,
			})
		}
	})

	// Liveness check endpoint - simple check for k8s/docker
	router.GET("/live", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "alive",
		})
	})

	go func() {
		log.Info().Str("port", port).Msg("HTTP server listening on /health, /ready, /live")
		if err := router.Run(":" + port); err != nil {
			log.Error().Err(err).Msg("HTTP server error")
		}
	}()

	return router
}
