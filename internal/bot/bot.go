package bot

import (
	"craig/internal/commands"
	"craig/internal/config"
	"craig/internal/scheduler"
	"fmt"
	"math/rand/v2"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Bot represents the Discord bot
type Bot struct {
	session              *discordgo.Session
	config               *config.Config
	commandModuleHandler *commands.ModuleHandler
	scheduler            *scheduler.Scheduler
	ready                atomic.Bool // guards interaction handling until startup completes
}

// New creates a new Bot instance
func New(cfg *config.Config) (*Bot, error) {
	// Create Discord session
	session, err := discordgo.New("Bot " + cfg.GetBotToken())
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	// Create modular command handler
	handler := commands.NewModuleHandler(cfg, session)

	bot := &Bot{
		session:              session,
		config:               cfg,
		commandModuleHandler: handler,
	}

	// mark not ready yet (zero value false, explicit for clarity)
	bot.ready.Store(false)

	// Set intents - we need guild, guild voice states, and message intents
	session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates

	// Add event handlers
	session.AddHandler(bot.onReady)

	// Slash commands
	session.AddHandler(bot.onInteractionCreate)

	return bot, nil
}

// Start starts the bot
func (b *Bot) Start() error {
	// Open connection
	err := b.session.Open()
	if err != nil {
		return fmt.Errorf("error opening Discord connection: %w", err)
	}
	defer func() {
		if err := b.session.Close(); err != nil {
			b.config.Logger.Warn("error closing Discord session:", err)
		}
	}()

	// Set bot status to "initializing"
	if err := b.session.UpdateGameStatus(0, "Starting up..."); err != nil {
		b.config.Logger.Warn("error updating bot status:", err)
	}

	// Register slash commands
	if err := b.commandModuleHandler.RegisterCommands(b.session); err != nil {
		return fmt.Errorf("error registering commands: %w", err)
	}

	// Initialize module services that need the Discord session
	if err := b.commandModuleHandler.InitializeModuleServices(b.session); err != nil {
		return fmt.Errorf("error initializing module services: %w", err)
	}

	// Create and initialize scheduler
	b.scheduler = scheduler.NewScheduler(b.session, b.config, b.commandModuleHandler.GetDB())

	// Register module schedulers (modules declare their own recurring tasks)
	b.commandModuleHandler.RegisterModuleSchedulers(b.scheduler)

	// Register config log rotation (not part of a module)
	b.scheduler.RegisterNewHourFunc(func() error {
		return b.config.RotateAndPruneLogs()
	})

	b.scheduler.Start()
	defer b.scheduler.Stop()

	// Update status to indicate the bot is ready
	if err := b.session.UpdateGameStatus(0, "Ready for pomodoro!"); err != nil {
		b.config.Logger.Warn("error updating bot status:", err)
	}

	// Signal readiness after all initialization steps complete.
	b.ready.Store(true)
	b.config.Logger.Info("Initialization complete; interactions enabled")
	b.config.Logger.Info("Craig bot is now running. Press CTRL+C to exit.")

	// Wait for interrupt signal
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Cleanup: Unregister commands, optionally
	if os.Getenv("UNREGISTER_COMMANDS") == "true" {
		b.commandModuleHandler.UnregisterCommands(b.session)
	}

	return nil
}

// onReady handles the ready event
func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	b.config.Logger.Infof("Bot received ready signal! Logged in as: %s#%s\n", r.User.Username, r.User.Discriminator)

	// Set bot status to something fresh every hour
	c := time.NewTicker(time.Hour)
	go func() {
		for range c.C {
			err := s.UpdateGameStatus(0, b.randomStatus())
			if err != nil {
				b.config.Logger.Warn("Error setting status:", err)
			}
		}
	}()
}

func (b *Bot) randomStatus() string {
	randomStuff := []string{
		"Pomodoro mode!",
		"Use /setup-pomo to start",
		"Focus time!",
		"Taking breaks...",
		"Lofi beats...",
		"Productivity helper",
	}

	return randomStuff[rand.IntN(len(randomStuff))]
}

// onInteractionCreate handles slash command interactions
func (b *Bot) onInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Initialization guard: reject interactions until startup has completed.
	if !b.ready.Load() {
		// Use the correct response type per interaction.
		switch i.Type {
		case discordgo.InteractionApplicationCommand, discordgo.InteractionMessageComponent:
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "⏳ Bot is starting up, try again in a few seconds.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
		case discordgo.InteractionPing:
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponsePong})
		default:
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "⏳ Bot is starting up, try again shortly.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
		}
		return
	}

	// Slash commands
	if i.Type == discordgo.InteractionApplicationCommand {
		if i.ApplicationCommandData().Name != "" {
			b.commandModuleHandler.HandleInteraction(s, i)
		}
		return
	}

	// Component interactions
	if i.Type == discordgo.InteractionMessageComponent {
		b.commandModuleHandler.HandleComponentInteraction(s, i)
		return
	}
}
