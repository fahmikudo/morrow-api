// Package assistant derives the pre-LLM replies and bridges to DeepSeek.
package assistant

import (
	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/wiretime"
)

// Request is the POST /assistant body.
type Request struct {
	Message string         `json:"message"`
	Today   *wiretime.Date `json:"today"`
}

// Response is the reply, which provider produced it, and what it is grounded in.
type Response struct {
	Reply    string      `json:"reply"`
	Provider string      `json:"provider"`
	Sources  []Source    `json:"sources"`
	Created  []task.Task `json:"created"`
	// Reason explains a derived fallback: ai_disabled, key_missing,
	// provider_error, empty_completion. Omitted for model answers.
	Reason string `json:"reason,omitempty"`
}

// Source is one context item a reply is grounded in.
type Source struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Href  string `json:"href"`
}

// Fallback reasons returned with a derived reply.
const (
	ReasonAIDisabled      = "ai_disabled"
	ReasonKeyMissing      = "key_missing"
	ReasonProviderError   = "provider_error"
	ReasonEmptyCompletion = "empty_completion"
)

// Fallback is returned when nothing matches and no model is configured.
const Fallback = "I can summarize your day, suggest what to focus on, or walk through a project. Try one of the suggestions."
