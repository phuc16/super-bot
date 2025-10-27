package activity

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// EventType represents the type of activity event
type EventType string

const (
	// EventTypeMessage represents a message creation event
	EventTypeMessage EventType = "message"
	// EventTypeVoiceJoin represents a user joining a voice channel
	EventTypeVoiceJoin EventType = "voice_join"
	// EventTypeVoiceLeave represents a user leaving a voice channel
	EventTypeVoiceLeave EventType = "voice_leave"
)

// ActivityEvent represents a single activity event
type ActivityEvent struct {
	Type      EventType
	GuildID   string
	UserID    string
	Username  string
	AvatarURL string
	ChannelID string
	Timestamp time.Time
}

// Tracker handles activity event tracking with buffered channel
type Tracker struct {
	db        *gorm.DB
	events    chan ActivityEvent
	processor *Processor
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewTracker creates a new activity tracker
func NewTracker(db *gorm.DB, bufferSize int) *Tracker {
	ctx, cancel := context.WithCancel(context.Background())

	tracker := &Tracker{
		db:     db,
		events: make(chan ActivityEvent, bufferSize),
		ctx:    ctx,
		cancel: cancel,
	}

	// Create processor
	tracker.processor = NewProcessor(db)

	return tracker
}

// Start begins processing activity events
func (t *Tracker) Start() {
	t.wg.Add(1)
	go t.run()
	log.Info().Msg("Activity tracker started")
}

// Track adds an activity event to the tracking queue
func (t *Tracker) Track(event ActivityEvent) error {
	// Set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case t.events <- event:
		return nil
	default:
		return fmt.Errorf("activity tracker buffer full")
	}
}

// run processes events from the channel
func (t *Tracker) run() {
	defer t.wg.Done()

	// Flush ticker - process batch every 5 seconds
	flushTicker := time.NewTicker(5 * time.Second)
	defer flushTicker.Stop()

	batch := make([]ActivityEvent, 0, 100)

	for {
		select {
		case <-t.ctx.Done():
			// Flush remaining events before shutdown
			if len(batch) > 0 {
				t.processBatch(batch)
			}
			log.Info().Msg("Activity tracker stopped")
			return

		case event := <-t.events:
			batch = append(batch, event)

			// Flush if batch is full
			if len(batch) >= 100 {
				t.processBatch(batch)
				batch = make([]ActivityEvent, 0, 100)
			}

		case <-flushTicker.C:
			// Flush batch on timer
			if len(batch) > 0 {
				t.processBatch(batch)
				batch = make([]ActivityEvent, 0, 100)
			}
		}
	}
}

// processBatch processes a batch of activity events
func (t *Tracker) processBatch(events []ActivityEvent) {
	start := time.Now()

	err := t.processor.ProcessBatch(t.ctx, events)
	if err != nil {
		log.Error().
			Err(err).
			Int("event_count", len(events)).
			Msg("Failed to process activity batch")
		return
	}

	log.Debug().
		Int("event_count", len(events)).
		Dur("duration", time.Since(start)).
		Msg("Activity batch processed")
}

// Shutdown gracefully shuts down the tracker
func (t *Tracker) Shutdown() error {
	log.Info().Msg("Shutting down activity tracker...")

	// Stop accepting new events
	t.cancel()

	// Wait for processing to complete
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()

	// Wait with timeout
	select {
	case <-done:
		log.Info().Msg("Activity tracker shutdown complete")
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("activity tracker shutdown timed out")
	}
}

// GetStats returns tracker statistics
func (t *Tracker) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"buffer_size":        cap(t.events),
		"buffer_used":        len(t.events),
		"buffer_utilization": float64(len(t.events)) / float64(cap(t.events)),
	}
}
