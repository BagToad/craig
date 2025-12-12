package commands

import (
	"craig/internal/commands/modules/ping"
	"craig/internal/commands/modules/pomo"
	"craig/internal/commands/types"
	"craig/internal/config"
	"craig/internal/database"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// ModuleHandler manages command modules, routing interactions and exposing select modules externally.
type ModuleHandler struct {
	commands map[string]*types.Command
	modules  map[string]types.CommandModule
	config   *config.Config
	db       *database.DB
	deps     *types.Dependencies
}

// NewModuleHandler creates a new module-based command handler
func NewModuleHandler(cfg *config.Config, session *discordgo.Session) *ModuleHandler {
	db, err := database.NewDB(cfg.GetDatabasePath())
	if err != nil {
		cfg.Logger.Warn("Warning: Failed to initialize database: %v", err)
	}

	h := &ModuleHandler{
		commands: make(map[string]*types.Command),
		modules:  make(map[string]types.CommandModule),
		config:   cfg,
		db:       db,
		deps: &types.Dependencies{
			Config:  cfg,
			DB:      db,
			Session: session,
		},
	}

	h.registerModules()

	return h
}

// registerModules registers all command modules
func (h *ModuleHandler) registerModules() {
	// Define modules with their constructors and names
	modules := []struct {
		name   string
		module types.CommandModule
	}{
		{"ping", ping.New(h.deps)},
		{"pomo", pomo.New(h.deps)},
	}

	for _, m := range modules {
		m.module.Register(h.commands, h.deps)
		h.modules[m.name] = m.module
	}
}

// GetModule returns a module by name with type assertion.
func (h *ModuleHandler) GetModule(name string) types.CommandModule {
	return h.modules[name]
}

// GetDB returns the database instance
func (h *ModuleHandler) GetDB() *database.DB {
	return h.db
}

// RegisterCommands registers all slash commands with Discord
func (h *ModuleHandler) RegisterCommands(s *discordgo.Session) error {
	existingCommands, err := s.ApplicationCommands(s.State.User.ID, "")
	if err != nil {
		h.config.Logger.Warn("Error fetching existing commands: %v", err)
		return err
	}

	existingByName := make(map[string]*discordgo.ApplicationCommand)
	for _, ec := range existingCommands {
		existingByName[ec.Name] = ec
	}

	for _, c := range h.commands {
		if c.Development {
			// Unregister development commands if they exist
			for _, existingCmd := range existingCommands {
				if existingCmd.Name == c.ApplicationCommand.Name {
					err := s.ApplicationCommandDelete(s.State.User.ID, "", existingCmd.ID)
					if err != nil {
						h.config.Logger.Warn("Error deleting command %s: %v", c.ApplicationCommand.Name, err)
					} else {
						h.config.Logger.Infof("Unregistered command: %s", c.ApplicationCommand.Name)
					}
				}
			}
			continue
		}

		if existing := existingByName[c.ApplicationCommand.Name]; existing != nil {
			cmd, err := s.ApplicationCommandEdit(s.State.User.ID, "", existing.ID, c.ApplicationCommand)
			if err != nil {
				return err
			}
			c.ApplicationCommand.ID = cmd.ID
			h.config.Logger.Infof("Updated command: %s", cmd.Name)
		} else {
			cmd, err := s.ApplicationCommandCreate(s.State.User.ID, "", c.ApplicationCommand)
			if err != nil {
				return err
			}
			c.ApplicationCommand.ID = cmd.ID
			h.config.Logger.Infof("Registered command: %s", cmd.Name)
		}
	}

	return nil
}

// HandleInteraction routes slash command interactions to appropriate handlers
func (h *ModuleHandler) HandleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.ApplicationCommandData().Name == "" {
		return
	}

	commandName := i.ApplicationCommandData().Name
	if cmd, exists := h.commands[commandName]; exists {
		cmd.HandlerFunc(s, i)
	}
}

// HandleComponentInteraction routes component interactions to appropriate module handlers
func (h *ModuleHandler) HandleComponentInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Check if pomo module handles this interaction
	if pomoMod, ok := h.GetModule("pomo").(*pomo.PomoModule); ok {
		pomoMod.HandleComponent(s, i)
	} else {
		h.config.Logger.Warn("Component interaction received but pomo module not available")
	}
}

// UnregisterCommands removes all registered commands
func (h *ModuleHandler) UnregisterCommands(s *discordgo.Session) {
	existingCommands, err := s.ApplicationCommands(s.State.User.ID, "")
	if err != nil {
		h.config.Logger.Warn("Error fetching existing commands: %v", err)
		return
	}

	for _, existingCmd := range existingCommands {
		if _, exists := h.commands[existingCmd.Name]; exists {
			err := s.ApplicationCommandDelete(s.State.User.ID, "", existingCmd.ID)
			if err != nil {
				h.config.Logger.Warn("Error deleting command %s: %v", existingCmd.Name, err)
			} else {
				h.config.Logger.Infof("Unregistered command: %s", existingCmd.Name)
			}
		}
	}
}

// InitializeModuleServices hydrates services with the Discord session.
// Called after the Discord session is established.
func (h *ModuleHandler) InitializeModuleServices(s *discordgo.Session) error {
	h.deps.Session = s

	// Hydrate services for all modules with the Discord session
	for _, module := range h.modules {
		if service := module.Service(); service != nil {
			if err := service.HydrateServiceDiscordSession(s); err != nil {
				return fmt.Errorf("failed to hydrate service with Discord session: %w", err)
			}
		}
	}

	return nil
}

// RegisterModuleSchedulers registers recurring tasks from all modules with the scheduler.
// Called after services are initialized.
func (h *ModuleHandler) RegisterModuleSchedulers(scheduler interface {
	RegisterNewMinuteFunc(fn func() error)
	RegisterNewHourFunc(fn func() error)
}) {
	for _, module := range h.modules {
		if service := module.Service(); service != nil {
			// Register minute functions
			if minuteFuncs := service.MinuteFuncs(); minuteFuncs != nil {
				for _, fn := range minuteFuncs {
					scheduler.RegisterNewMinuteFunc(fn)
				}
			}

			// Register hour functions
			if hourFuncs := service.HourFuncs(); hourFuncs != nil {
				for _, fn := range hourFuncs {
					scheduler.RegisterNewHourFunc(fn)
				}
			}
		}
	}
}
