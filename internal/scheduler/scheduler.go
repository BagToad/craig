package scheduler

import (
	"craig/internal/config"
	"craig/internal/database"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Scheduler manages scheduled tasks
type Scheduler struct {
	session     *discordgo.Session
	config      *config.Config
	db          *database.DB
	minuteFuncs []func() error
	hourFuncs   []func() error
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// NewScheduler creates a new scheduler
func NewScheduler(session *discordgo.Session, cfg *config.Config, db *database.DB) *Scheduler {
	return &Scheduler{
		session:     session,
		config:      cfg,
		db:          db,
		minuteFuncs: []func() error{},
		hourFuncs:   []func() error{},
		stopChan:    make(chan struct{}),
	}
}

// RegisterNewMinuteFunc registers a function to be called every minute
func (s *Scheduler) RegisterNewMinuteFunc(fn func() error) {
	s.minuteFuncs = append(s.minuteFuncs, fn)
}

// RegisterNewHourFunc registers a function to be called every hour
func (s *Scheduler) RegisterNewHourFunc(fn func() error) {
	s.hourFuncs = append(s.hourFuncs, fn)
}

// Start starts the scheduler
func (s *Scheduler) Start() {
	s.wg.Add(2)

	// Start minute ticker
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				for _, fn := range s.minuteFuncs {
					if err := fn(); err != nil {
						s.config.Logger.Errorf("Error in minute function: %v", err)
					}
				}
			case <-s.stopChan:
				return
			}
		}
	}()

	// Start hour ticker
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				for _, fn := range s.hourFuncs {
					if err := fn(); err != nil {
						s.config.Logger.Errorf("Error in hour function: %v", err)
					}
				}
			case <-s.stopChan:
				return
			}
		}
	}()

	s.config.Logger.Info("Scheduler started")
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	close(s.stopChan)
	s.wg.Wait()
	s.config.Logger.Info("Scheduler stopped")
}
