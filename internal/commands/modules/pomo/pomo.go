package pomo

import (
	"craig/internal/commands/types"
	"craig/internal/database"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// PomoModule implements the CommandModule interface for the pomodoro command
type PomoModule struct {
	deps     *types.Dependencies
	sessions map[string]*pomoSessionState // messageID -> state
}

type pomoSessionState struct {
	voiceConn      *discordgo.VoiceConnection
	stopPlayback   chan struct{}
	ticker         *time.Ticker
	lastUpdateTime time.Time
}

// New creates a new pomo module
func New(deps *types.Dependencies) *PomoModule {
	return &PomoModule{
		deps:     deps,
		sessions: make(map[string]*pomoSessionState),
	}
}

// Register adds the pomo command to the command map
func (m *PomoModule) Register(cmds map[string]*types.Command, deps *types.Dependencies) {
	cmds["setup-pomo"] = &types.Command{
		ApplicationCommand: &discordgo.ApplicationCommand{
			Name:                     "setup-pomo",
			Description:              "Create a pomodoro panel (Admin only)",
			DefaultMemberPermissions: &[]int64{discordgo.PermissionAdministrator}[0],
		},
		HandlerFunc: m.handleSetupPomo,
	}
}

// handleSetupPomo handles the /setup-pomo command
func (m *PomoModule) handleSetupPomo(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Respond with the pomo panel
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       "🍅 Pomodoro Timer",
					Description: "Click **Start** to begin a pomodoro session.\nThe bot will join your voice channel and play lofi music.\n\n**Timer:** Not started\n**Status:** ⏹️ Stopped",
					Color:       0x3498db,
					Footer: &discordgo.MessageEmbedFooter{
						Text: "Focus and be productive!",
					},
				},
			},
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{
							Label:    "Start",
							Style:    discordgo.SuccessButton,
							CustomID: "pomo_start",
							Emoji: &discordgo.ComponentEmoji{
								Name: "▶️",
							},
						},
						discordgo.Button{
							Label:    "Pause",
							Style:    discordgo.SecondaryButton,
							CustomID: "pomo_pause",
							Emoji: &discordgo.ComponentEmoji{
								Name: "⏸️",
							},
							Disabled: true,
						},
						discordgo.Button{
							Label:    "Stop",
							Style:    discordgo.DangerButton,
							CustomID: "pomo_stop",
							Emoji: &discordgo.ComponentEmoji{
								Name: "⏹️",
							},
							Disabled: true,
						},
					},
				},
			},
		},
	})

	if err != nil {
		m.deps.Config.Logger.Errorf("Failed to send pomo panel: %v", err)
		return
	}

	// Get the message to store it in the database
	// We need to fetch the original response to get the message ID
	go func() {
		time.Sleep(500 * time.Millisecond) // Give Discord time to create the message
		msg, err := s.InteractionResponse(i.Interaction)
		if err != nil {
			m.deps.Config.Logger.Errorf("Failed to get interaction response: %v", err)
			return
		}

		// Store in database
		_, err = m.deps.DB.CreatePomoSession(msg.ID, msg.ChannelID, i.GuildID)
		if err != nil {
			m.deps.Config.Logger.Errorf("Failed to create pomo session in database: %v", err)
		}
	}()
}

// HandleComponent handles button interactions for the pomo panel
func (m *PomoModule) HandleComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID

	// Check if this is a pomo button
	if !strings.HasPrefix(customID, "pomo_") {
		return
	}

	// Get the session from the database
	session, err := m.deps.DB.GetPomoSessionByMessageID(i.Message.ID)
	if err != nil {
		m.deps.Config.Logger.Errorf("Failed to get pomo session: %v", err)
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Failed to find pomodoro session.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	switch customID {
	case "pomo_start":
		m.handleStart(s, i, session)
	case "pomo_pause":
		m.handlePause(s, i, session)
	case "pomo_stop":
		m.handleStop(s, i, session)
	}
}

// handleStart starts the pomodoro timer and joins voice
func (m *PomoModule) handleStart(s *discordgo.Session, i *discordgo.InteractionCreate, session *database.PomoSession) {
	// Find the user's voice channel
	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		m.deps.Config.Logger.Errorf("Failed to get guild: %v", err)
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Failed to find the server.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	var voiceChannelID string
	for _, vs := range guild.VoiceStates {
		if vs.UserID == i.Member.User.ID {
			voiceChannelID = vs.ChannelID
			break
		}
	}

	if voiceChannelID == "" {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ You must be in a voice channel to start a pomodoro session!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// If already running or paused, resume
	if session.State == "paused" {
		m.resumeSession(s, i, session)
		return
	}

	// Acknowledge the interaction first
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	// Update session in database
	now := time.Now()
	session.State = "running"
	session.UserID = i.Member.User.ID
	session.VoiceChannelID = voiceChannelID
	session.StartTime = &now
	session.ElapsedSeconds = 0

	if err := m.deps.DB.UpdatePomoSession(session); err != nil {
		m.deps.Config.Logger.Errorf("Failed to update pomo session: %v", err)
		return
	}

	// Join voice channel
	vc, err := s.ChannelVoiceJoin(i.GuildID, voiceChannelID, false, true)
	if err != nil {
		m.deps.Config.Logger.Errorf("Failed to join voice channel: %v", err)
		return
	}

	// Start playing lofi music (placeholder for now)
	go m.playAudio(vc)

	// Create session state
	stopChan := make(chan struct{})
	ticker := time.NewTicker(1 * time.Second)

	m.sessions[session.MessageID] = &pomoSessionState{
		voiceConn:      vc,
		stopPlayback:   stopChan,
		ticker:         ticker,
		lastUpdateTime: now,
	}

	// Start timer update goroutine
	go m.updateTimer(s, session, ticker, stopChan)

	// Update the embed
	m.updateEmbed(s, i.Message.ChannelID, i.Message.ID, session, "running")

	m.deps.Config.Logger.Infof("Started pomo session for user %s in channel %s", i.Member.User.ID, voiceChannelID)
}

// resumeSession resumes a paused session
func (m *PomoModule) resumeSession(s *discordgo.Session, i *discordgo.InteractionCreate, session *database.PomoSession) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	now := time.Now()
	session.State = "running"
	session.StartTime = &now

	if err := m.deps.DB.UpdatePomoSession(session); err != nil {
		m.deps.Config.Logger.Errorf("Failed to update pomo session: %v", err)
		return
	}

	// Get session state
	state := m.sessions[session.MessageID]
	if state != nil {
		state.lastUpdateTime = now
	}

	m.updateEmbed(s, i.Message.ChannelID, i.Message.ID, session, "running")
	m.deps.Config.Logger.Infof("Resumed pomo session for message %s", session.MessageID)
}

// handlePause pauses the pomodoro timer
func (m *PomoModule) handlePause(s *discordgo.Session, i *discordgo.InteractionCreate, session *database.PomoSession) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	// Calculate elapsed time since last update
	state := m.sessions[session.MessageID]
	if state != nil && session.StartTime != nil {
		elapsed := time.Since(state.lastUpdateTime)
		session.ElapsedSeconds += int(elapsed.Seconds())
	}

	session.State = "paused"
	session.StartTime = nil

	if err := m.deps.DB.UpdatePomoSession(session); err != nil {
		m.deps.Config.Logger.Errorf("Failed to update pomo session: %v", err)
		return
	}

	m.updateEmbed(s, i.Message.ChannelID, i.Message.ID, session, "paused")
	m.deps.Config.Logger.Infof("Paused pomo session for message %s", session.MessageID)
}

// handleStop stops the pomodoro timer and disconnects from voice
func (m *PomoModule) handleStop(s *discordgo.Session, i *discordgo.InteractionCreate, session *database.PomoSession) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	// Stop the session
	state := m.sessions[session.MessageID]
	if state != nil {
		close(state.stopPlayback)
		state.ticker.Stop()
		if state.voiceConn != nil {
			state.voiceConn.Disconnect()
		}
		delete(m.sessions, session.MessageID)
	}

	// Update database
	session.State = "stopped"
	session.StartTime = nil
	session.ElapsedSeconds = 0
	session.VoiceChannelID = ""

	if err := m.deps.DB.UpdatePomoSession(session); err != nil {
		m.deps.Config.Logger.Errorf("Failed to update pomo session: %v", err)
		return
	}

	m.updateEmbed(s, i.Message.ChannelID, i.Message.ID, session, "stopped")
	m.deps.Config.Logger.Infof("Stopped pomo session for message %s", session.MessageID)
}

// updateTimer updates the timer every second
func (m *PomoModule) updateTimer(s *discordgo.Session, session *database.PomoSession, ticker *time.Ticker, stopChan chan struct{}) {
	updateCount := 0
	for {
		select {
		case <-ticker.C:
			updateCount++
			// Update session in memory
			state := m.sessions[session.MessageID]
			if state != nil && session.State == "running" {
				elapsed := time.Since(state.lastUpdateTime)
				session.ElapsedSeconds += int(elapsed.Seconds())
				state.lastUpdateTime = time.Now()

				// Update embed every 5 seconds to avoid rate limits
				if updateCount%5 == 0 {
					// Save to database periodically
					if err := m.deps.DB.UpdatePomoSession(session); err != nil {
						m.deps.Config.Logger.Errorf("Failed to update pomo session: %v", err)
					}
					// Update the display
					m.updateEmbed(s, session.ChannelID, session.MessageID, session, "running")
				}
			}
		case <-stopChan:
			return
		}
	}
}

// updateEmbed updates the embed with current timer state
func (m *PomoModule) updateEmbed(s *discordgo.Session, channelID, messageID string, session *database.PomoSession, state string) {
	// Format time
	minutes := session.ElapsedSeconds / 60
	seconds := session.ElapsedSeconds % 60
	timeStr := fmt.Sprintf("%02d:%02d", minutes, seconds)

	// Status emoji
	statusEmoji := "⏹️"
	statusText := "Stopped"
	switch state {
	case "running":
		statusEmoji = "▶️"
		statusText = "Running"
	case "paused":
		statusEmoji = "⏸️"
		statusText = "Paused"
	}

	description := fmt.Sprintf("Click **Start** to begin a pomodoro session.\nThe bot will join your voice channel and play lofi music.\n\n**Timer:** %s\n**Status:** %s %s", timeStr, statusEmoji, statusText)

	// Button states
	startDisabled := state == "running"
	pauseDisabled := state != "running"
	stopDisabled := state == "stopped"

	_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: channelID,
		ID:      messageID,
		Embeds: &[]*discordgo.MessageEmbed{
			{
				Title:       "🍅 Pomodoro Timer",
				Description: description,
				Color:       0x3498db,
				Footer: &discordgo.MessageEmbedFooter{
					Text: "Focus and be productive!",
				},
			},
		},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Start",
						Style:    discordgo.SuccessButton,
						CustomID: "pomo_start",
						Emoji: &discordgo.ComponentEmoji{
							Name: "▶️",
						},
						Disabled: startDisabled,
					},
					discordgo.Button{
						Label:    "Pause",
						Style:    discordgo.SecondaryButton,
						CustomID: "pomo_pause",
						Emoji: &discordgo.ComponentEmoji{
							Name: "⏸️",
						},
						Disabled: pauseDisabled,
					},
					discordgo.Button{
						Label:    "Stop",
						Style:    discordgo.DangerButton,
						CustomID: "pomo_stop",
						Emoji: &discordgo.ComponentEmoji{
							Name: "⏹️",
						},
						Disabled: stopDisabled,
					},
				},
			},
		},
	})

	if err != nil {
		m.deps.Config.Logger.Errorf("Failed to update embed: %v", err)
	}
}

// Service returns nil as this module has no scheduled services
func (m *PomoModule) Service() types.ModuleService {
	return nil
}
