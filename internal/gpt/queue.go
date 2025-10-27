package gpt

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Request represents a queued GPT summarization request
type Request struct {
	ID               uuid.UUID
	FormattedMessage string
	ChannelName      string
	Language         string // "vi" or "en"
	ResponseChan     chan Response
}

// Response represents the result of a GPT request
type Response struct {
	Summary string
	Tokens  int
	Status  string
	Error   error
}

// Queue manages concurrent GPT requests
type Queue struct {
	requests   chan Request
	workers    int
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	summarizer *Summarizer
}

// NewQueue creates a new GPT request queue
func NewQueue(workers int, summarizer *Summarizer) *Queue {
	ctx, cancel := context.WithCancel(context.Background())

	q := &Queue{
		requests:   make(chan Request, 100), // Buffer up to 100 requests
		workers:    workers,
		ctx:        ctx,
		cancel:     cancel,
		summarizer: summarizer,
	}

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}

	log.Info().Int("workers", workers).Msg("GPT queue started")
	return q
}

// Submit adds a request to the queue
func (q *Queue) Submit(formattedMessage string, channelName string) (uuid.UUID, chan Response) {
	return q.SubmitWithLanguage(formattedMessage, channelName, "vi") // Default to Vietnamese
}

// SubmitWithLanguage adds a request to the queue with specified language
func (q *Queue) SubmitWithLanguage(formattedMessage string, channelName string, language string) (uuid.UUID, chan Response) {
	requestID := uuid.New()
	responseChan := make(chan Response, 1)

	req := Request{
		ID:               requestID,
		FormattedMessage: formattedMessage,
		ChannelName:      channelName,
		Language:         language,
		ResponseChan:     responseChan,
	}

	// Non-blocking send (with timeout handled by caller)
	select {
	case q.requests <- req:
		log.Debug().Str("request_id", requestID.String()).Msg("Request queued")
	case <-q.ctx.Done():
		responseChan <- Response{Error: ErrQueueClosed}
	default:
		// Queue is full
		responseChan <- Response{Error: ErrQueueFull}
	}

	return requestID, responseChan
}

// GetQueueSize returns the current queue size
func (q *Queue) GetQueueSize() int {
	return len(q.requests)
}

// worker processes requests from the queue
func (q *Queue) worker(id int) {
	defer q.wg.Done()

	log.Debug().Int("worker_id", id).Msg("GPT worker started")

	for {
		select {
		case <-q.ctx.Done():
			log.Debug().Int("worker_id", id).Msg("GPT worker stopping")
			return
		case req := <-q.requests:
			q.processRequest(req)
		}
	}
}

// processRequest handles a single request
func (q *Queue) processRequest(req Request) {
	log.Debug().
		Str("request_id", req.ID.String()).
		Msg("Processing GPT request")

	// Create context with timeout
	ctx, cancel := context.WithTimeout(q.ctx, GPTRequestTimeout)
	defer cancel()

	// Set language for this request
	q.summarizer.SetLanguage(req.Language)

	// Call summarizer with fallback
	summary, tokens, status, err := q.summarizer.SummarizeWithFallback(
		ctx,
		req.FormattedMessage,
		req.ChannelName,
	)

	// Send response
	req.ResponseChan <- Response{
		Summary: summary,
		Tokens:  tokens,
		Status:  status,
		Error:   err,
	}
	close(req.ResponseChan)

	log.Debug().
		Str("request_id", req.ID.String()).
		Str("status", status).
		Int("tokens", tokens).
		Msg("GPT request completed")
}

// Shutdown gracefully stops the queue
func (q *Queue) Shutdown() {
	log.Info().Msg("Shutting down GPT queue...")
	q.cancel()
	q.wg.Wait()
	close(q.requests)
	log.Info().Msg("GPT queue shut down")
}

// Queue errors
var (
	ErrQueueFull   = &QueueError{Message: "queue is full"}
	ErrQueueClosed = &QueueError{Message: "queue is closed"}
)

// QueueError represents a queue-specific error
type QueueError struct {
	Message string
}

func (e *QueueError) Error() string {
	return e.Message
}
