// Package chat manages a stateful conversation session.
package chat

import (
	"context"
	"fmt"

	"llm-chat/internal/anthropic"
)

// Session keeps track of the full conversation history and drives the
// request/response cycle.
type Session struct {
	client       *anthropic.Client
	history      []anthropic.Message
	systemPrompt string
	usage        anthropic.Usage // cumulative token counts
}

// NewSession creates a Session backed by the given client.
func NewSession(client *anthropic.Client, systemPrompt string) *Session {
	return &Session{client: client, systemPrompt: systemPrompt}
}

// Stream appends a plain-text user message, streams the reply, and records usage.
func (s *Session) Stream(ctx context.Context, userInput string, onToken func(string)) error {
	msg := anthropic.NewTextMessage(anthropic.RoleUser, userInput)
	s.history = append(s.history, msg)

	reply, usage, err := s.client.Stream(ctx, s.history, s.systemPrompt, onToken)
	if err != nil {
		s.history = s.history[:len(s.history)-1]
		return fmt.Errorf("send: %w", err)
	}

	s.history = append(s.history, anthropic.NewTextMessage(anthropic.RoleAssistant, reply))
	s.usage.InputTokens += usage.InputTokens
	s.usage.OutputTokens += usage.OutputTokens
	return nil
}

// StreamWithImage appends a user message with an image, streams the reply.
func (s *Session) StreamWithImage(ctx context.Context, imagePath, caption string, onToken func(string)) error {
	msg, err := anthropic.NewImageMessage(anthropic.RoleUser, imagePath, caption)
	if err != nil {
		return fmt.Errorf("load image: %w", err)
	}
	s.history = append(s.history, msg)

	reply, usage, err := s.client.Stream(ctx, s.history, s.systemPrompt, onToken)
	if err != nil {
		s.history = s.history[:len(s.history)-1]
		return fmt.Errorf("send: %w", err)
	}

	s.history = append(s.history, anthropic.NewTextMessage(anthropic.RoleAssistant, reply))
	s.usage.InputTokens += usage.InputTokens
	s.usage.OutputTokens += usage.OutputTokens
	return nil
}

// Usage returns the cumulative token usage for this session.
func (s *Session) Usage() anthropic.Usage { return s.usage }

// Reset clears history and usage counters.
func (s *Session) Reset() {
	s.history = nil
	s.usage = anthropic.Usage{}
}

// History returns a read-only copy of the current conversation turns.
func (s *Session) History() []anthropic.Message {
	out := make([]anthropic.Message, len(s.history))
	copy(out, s.history)
	return out
}
