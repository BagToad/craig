# Craig - Pomodoro Discord Bot

A Discord bot that helps users stay focused with pomodoro sessions. Craig joins voice channels and plays lofi music to help you concentrate!

## Features

- `/setup-pomo` - Creates an interactive pomodoro panel with:
  - **Start** button - Begins a pomodoro session, bot joins your voice channel and plays lofi music
  - **Pause** button - Pauses the timer and music
  - **Stop** button - Stops the timer and disconnects from voice

The bot tracks time and provides a clean interface for managing focus sessions.

## Architecture

Craig follows a modular architecture inspired by the BestPal bot:

### Core Structure
- `cmd/craig/` - Main application entry point
- `internal/bot/` - Bot lifecycle and Discord session management
- `internal/config/` - Configuration management using Viper
- `internal/database/` - SQLite database for session persistence
- `internal/commands/` - Command module system
  - `types/` - Interfaces for modules and services
  - `modules/` - Individual command modules (ping, pomo)
- `internal/scheduler/` - Background task scheduler

### Module Pattern
Each module implements:
1. `New(deps *types.Dependencies)` - Constructor with shared dependencies
2. `Register(cmds map[string]*types.Command, deps *types.Dependencies)` - Command registration
3. `Service() types.ModuleService` - Optional background service (returns nil if not needed)

## Quick Start

### Prerequisites
- Go 1.24+
- Discord bot token with Voice permissions
- SQLite3

### Installation

1. Clone the repository:
```bash
git clone https://github.com/BagToad/craig.git
cd craig
```

2. Copy the example config:
```bash
cp config.example.yaml config.yaml
```

3. Edit `config.yaml` with your bot token and server ID

4. Build the bot:
```bash
make build
```

5. Run the bot:
```bash
./bin/craig
```

Or use `make run` to build and run in one step.

## Configuration

Edit `config.yaml`:

```yaml
bot_token: "your-discord-bot-token"
server_id: "your-server-id"
crypto_salt: "random-string-for-hashing"
super_admins:
  - "your-user-id"
```

Environment variables (override config file):
- `CRAIG_BOT_TOKEN` - Discord bot token
- `CRAIG_LOG_DIR` - Log directory (default: ./logs)

## Development

### Building
```bash
make build        # Build for current platform
make build-all    # Build for all platforms
make clean        # Clean build artifacts
```

### Testing
```bash
make test         # Run tests
```

### Project Structure
```
craig/
├── cmd/
│   └── craig/          # Main application
├── internal/
│   ├── bot/            # Bot core
│   ├── config/         # Configuration
│   ├── database/       # Database layer
│   ├── commands/       # Command system
│   │   ├── types/      # Interfaces
│   │   └── modules/    # Command modules
│   │       ├── ping/   # Ping command
│   │       └── pomo/   # Pomodoro module
│   └── scheduler/      # Task scheduler
├── .github/
│   └── workflows/      # CI/CD pipelines
├── go.mod
├── Makefile
└── README.md
```

## Adding a New Module

1. Create a new directory under `internal/commands/modules/<name>/`
2. Implement the module following the pattern:

```go
package mymodule

import (
    "craig/internal/commands/types"
    "github.com/bwmarrin/discordgo"
)

type MyModule struct {
    deps *types.Dependencies
}

func New(deps *types.Dependencies) *MyModule {
    return &MyModule{deps: deps}
}

func (m *MyModule) Register(cmds map[string]*types.Command, deps *types.Dependencies) {
    cmds["mycommand"] = &types.Command{
        ApplicationCommand: &discordgo.ApplicationCommand{
            Name:        "mycommand",
            Description: "My command description",
        },
        HandlerFunc: m.handleMyCommand,
    }
}

func (m *MyModule) handleMyCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
    // Handle the command
}

func (m *MyModule) Service() types.ModuleService {
    return nil // or return a service if needed
}
```

3. Register the module in `internal/commands/module_handler.go`

## Deployment

The bot uses GitHub Actions for CI/CD:
- Pushes to `main` trigger production deployment
- CodeQL scans run on schedule and PRs

## License

Copyright (c) 2025. All rights reserved.

## Support

For issues or questions, please open an issue on GitHub.