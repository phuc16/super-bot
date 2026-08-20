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
	aitopicService "github.com/phuc16/VCG-super-bot/internal/services/aitopic"
	configService "github.com/phuc16/VCG-super-bot/internal/services/config"
	"github.com/phuc16/VCG-super-bot/internal/services/customcmd"
	"github.com/phuc16/VCG-super-bot/internal/services/duelstats"
	"github.com/phuc16/VCG-super-bot/internal/services/export"
	"github.com/phuc16/VCG-super-bot/internal/storage"
	"github.com/phuc16/VCG-super-bot/internal/storage/migrations"
	"github.com/phuc16/VCG-super-bot/pkg/logger"
)

func main() {
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

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logger.Init(cfg.LogLevel)
	log.Info().Msg("Discord Super Bot starting...")
	log.Info().
		Str("environment", cfg.AppEnv).
		Str("log_level", cfg.LogLevel).
		Bool("debug_mode", cfg.DebugMode).
		Bool("mock_gpt", cfg.MockGPTAPI).
		Msg("Configuration loaded and validated successfully")

	defer commands.CloseAllRateLimiters()

	log.Info().Msg("Connecting to database...")
	if err := storage.ConnectWithMigrations(cfg.GetDSN(), cfg.DebugMode); err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}
	defer storage.Close()
	storage.LogConnectionStats()

	log.Info().Msg("Initializing Discord client...")
	discordClient, err := initializeDiscord(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize Discord: %w", err)
	}
	defer discordClient.Close()

	_ = startHealthServer(cfg.Port, storage.GetDB(), discordClient, cfg)

	log.Info().Msg("Initializing GPT client...")
	gptClient, gptQueue := initializeGPT(cfg)
	defer gptQueue.Shutdown()

	log.Info().Msg("Initializing services...")
	exportSvc, configSvc, customcmdSvc, aiTopicSvc, duelStatsSvc, taskScheduler := initializeServices(storage.GetDB(), discordClient, gptClient, gptQueue, cfg)

	log.Info().Msg("Registering event handlers...")
	messageHandler := events.NewMessageHandler(customcmdSvc)
	messageHandler.Register(discordClient.Session)

	log.Info().Msg("Starting task scheduler...")
	if err := taskScheduler.Start(); err != nil {
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	log.Info().Msg("Registering Discord commands...")
	if err := registerCommands(discordClient, exportSvc, configSvc, customcmdSvc, aiTopicSvc, duelStatsSvc, gptClient, cfg); err != nil {
		return fmt.Errorf("failed to register commands: %w", err)
	}

	log.Info().Msg("✅ Bot is ready!")

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
	logger.LogMetricsSummary()
	storage.LogConnectionStats()
	taskScheduler.Stop()

	return nil
}

func runMigrations() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: bot migrate [up|down|version]")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger.Init(cfg.LogLevel)

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
	costTracker := gpt.NewCostTracker(cfg.GPTDailyBudgetUSD)
	gptClient := gpt.NewClient(cfg.OpenAIAPIKey, cfg.GPTModel, cfg.GPTMaxTokens, cfg.MockGPTAPI, costTracker)
	summarizer := gpt.NewSummarizer(gptClient)
	queue := gpt.NewQueue(cfg.GPTMaxConcurrentRequests, summarizer)
	return gptClient, queue
}

func initializeServices(db *gorm.DB, discordClient *discord.Client, gptClient *gpt.Client, gptQueue *gpt.Queue, cfg *config.Config) (*export.Service, *configService.Service, *customcmd.Service, *aitopicService.Service, *duelstats.Service, *scheduler.Scheduler) {
	exportSvc := export.NewService(db, discordClient.Session, gptQueue)
	configSvc := configService.NewService(db)
	customcmdSvc := customcmd.NewService(db)
	aiTopicSvc := aitopicService.NewService(db)
	duelStatsSvc := duelstats.NewService(db)
	taskScheduler := scheduler.NewScheduler(db, discordClient.Session, aiTopicSvc, gptClient, cfg.DiscordGuildID)
	return exportSvc, configSvc, customcmdSvc, aiTopicSvc, duelStatsSvc, taskScheduler
}

func registerCommands(discordClient *discord.Client, exportSvc *export.Service, configSvc *configService.Service, customcmdSvc *customcmd.Service, aiTopicSvc *aitopicService.Service, duelStatsSvc *duelstats.Service, gptClient *gpt.Client, cfg *config.Config) error {
	rateLimiter := export.NewRateLimiter(cfg.RateLimitExportPerHour)

	exportCmd := commands.NewExportCommand(exportSvc, rateLimiter, cfg.Timezone)
	configCmd := commands.NewConfigCommand(configSvc)
	helpCmd := commands.NewHelpCommand()
	addcomCmd := commands.NewAddcomCommand(customcmdSvc)
	delcomCmd := commands.NewDelcomCommand(customcmdSvc)
	listcomCmd := commands.NewListcomCommand(customcmdSvc)
	addtopicCmd := commands.NewAddtopicCommand(aiTopicSvc)
	deltopicCmd := commands.NewDeltopicCommand(aiTopicSvc)
	listtopicCmd := commands.NewListtopicCommand(aiTopicSvc)
	posttopicCmd := commands.NewPosttopicCommand(gptClient)
	roastCmd := commands.NewRoastCommand(gptClient)
	duelCmd := commands.NewDuelCommand(duelStatsSvc)
	duelLeaderboardCmd := commands.NewDuelLeaderboardCommand(duelStatsSvc)
	duelHistoryCmd := commands.NewDuelHistoryCommand(duelStatsSvc)
	duelResetCmd := commands.NewDuelResetCommand(duelStatsSvc)
	duelChallengeAllCmd := commands.NewDuelChallengeAllCommand(duelStatsSvc, cfg.Timezone)

	for _, def := range []*discordgo.ApplicationCommand{
		exportCmd.Definition(),
		configCmd.Definition(),
		helpCmd.Definition(),
		addcomCmd.Definition(),
		delcomCmd.Definition(),
		listcomCmd.Definition(),
		addtopicCmd.Definition(),
		deltopicCmd.Definition(),
		listtopicCmd.Definition(),
		posttopicCmd.Definition(),
		roastCmd.Definition(),
		duelCmd.Definition(),
		duelLeaderboardCmd.Definition(),
		duelHistoryCmd.Definition(),
		duelResetCmd.Definition(),
		duelChallengeAllCmd.Definition(),
	} {
		if err := discordClient.RegisterCommand(def); err != nil {
			return fmt.Errorf("failed to register /%s command: %w", def.Name, err)
		}
	}

	log.Info().Msg("Registered 16 Discord commands: /export, /config, /help, /addcom, /delcom, /listcom, /addtopic, /deltopic, /listtopic, /posttopic, /roast, /duel, /duel-leaderboard, /duel-history, /duel-reset, /duel-challenge-all")

	wrap := func(name string, h commands.CommandHandler, limiters ...func(commands.CommandHandler) commands.CommandHandler) commands.CommandHandler {
		mw := append(limiters,
			func(h commands.CommandHandler) commands.CommandHandler { return commands.WithRecovery(name, h) },
			func(h commands.CommandHandler) commands.CommandHandler { return commands.WithLogging(name, h) },
		)
		return commands.ChainMiddleware(h, mw...)
	}

	exportHandler := wrap("export", exportCmd.Handle, func(h commands.CommandHandler) commands.CommandHandler {
		return commands.WithUserRateLimit("export", commands.GlobalRateLimiters.Export, h)
	})
	configHandler := wrap("config", configCmd.Handle, func(h commands.CommandHandler) commands.CommandHandler {
		return commands.WithUserRateLimit("config", commands.GlobalRateLimiters.Config, h)
	})
	helpHandler := wrap("help", helpCmd.Handle)
	addcomHandler := wrap("addcom", addcomCmd.Handle)
	delcomHandler := wrap("delcom", delcomCmd.Handle)
	listcomHandler := wrap("listcom", listcomCmd.Handle)
	addtopicHandler := wrap("addtopic", addtopicCmd.Handle)
	deltopicHandler := wrap("deltopic", deltopicCmd.Handle)
	listtopicHandler := wrap("listtopic", listtopicCmd.Handle)
	posttopicHandler := wrap("posttopic", posttopicCmd.Handle)
	roastHandler := wrap("roast", roastCmd.Handle, func(h commands.CommandHandler) commands.CommandHandler {
		return commands.WithUserRateLimit("roast", commands.GlobalRateLimiters.Roast, h)
	})
	duelHandler := wrap("duel", duelCmd.Handle, func(h commands.CommandHandler) commands.CommandHandler {
		return commands.WithUserRateLimit("duel", commands.GlobalRateLimiters.Duel, h)
	})
	duelLeaderboardHandler := wrap("duel-leaderboard", duelLeaderboardCmd.Handle)
	duelHistoryHandler := wrap("duel-history", duelHistoryCmd.Handle)
	duelResetHandler := wrap("duel-reset", duelResetCmd.Handle)
	duelChallengeAllHandler := wrap("duel-challenge-all", duelChallengeAllCmd.Handle)

	discordClient.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type == discordgo.InteractionMessageComponent {
			duelCmd.HandleComponent(s, i)
			duelResetCmd.HandleComponent(s, i)
			duelChallengeAllCmd.HandleComponent(s, i)
			return
		}
		if i.Type == discordgo.InteractionModalSubmit {
			duelCmd.HandleModalSubmit(s, i)
			duelChallengeAllCmd.HandleModalSubmit(s, i)
			return
		}
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		switch i.ApplicationCommandData().Name {
		case "export":
			exportHandler(s, i)
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
		case "posttopic":
			posttopicHandler(s, i)
		case "roast":
			roastHandler(s, i)
		case "duel":
			duelHandler(s, i)
		case "duel-leaderboard":
			duelLeaderboardHandler(s, i)
		case "duel-history":
			duelHistoryHandler(s, i)
		case "duel-reset":
			duelResetHandler(s, i)
		case "duel-challenge-all":
			duelChallengeAllHandler(s, i)
		default:
			log.Warn().Str("command", i.ApplicationCommandData().Name).Msg("Unknown command received")
		}
	})

	return nil
}

func startHealthServer(port string, db *gorm.DB, discordClient *discord.Client, cfg *config.Config) *gin.Engine {
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		health := gin.H{"status": "healthy", "timestamp": time.Now().Format(time.RFC3339), "checks": gin.H{}}
		overallHealthy := true

		sqlDB, err := db.DB()
		if err != nil || sqlDB.Ping() != nil {
			health["checks"].(gin.H)["database"] = gin.H{"status": "unhealthy"}
			overallHealthy = false
		} else {
			health["checks"].(gin.H)["database"] = gin.H{"status": "healthy"}
		}

		if discordClient != nil && discordClient.Session != nil && discordClient.Session.DataReady {
			health["checks"].(gin.H)["discord"] = gin.H{"status": "healthy", "connected": true}
		} else {
			health["checks"].(gin.H)["discord"] = gin.H{"status": "degraded", "connected": false}
		}

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

	router.GET("/ready", func(c *gin.Context) {
		sqlDB, err := db.DB()
		dbReady := err == nil && sqlDB.Ping() == nil
		discordReady := discordClient != nil && discordClient.Session != nil && discordClient.Session.DataReady

		if dbReady && discordReady {
			c.JSON(200, gin.H{"status": "ready"})
		} else {
			c.JSON(503, gin.H{"status": "not ready"})
		}
	})

	router.GET("/live", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "alive"})
	})

	go func() {
		log.Info().Str("port", port).Msg("HTTP server listening on /health, /ready, /live")
		if err := router.Run(":" + port); err != nil {
			log.Error().Err(err).Msg("HTTP server error")
		}
	}()

	return router
}
