package settings

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"morrow-api/internal/config"
)

// catalogueTTL bounds how often the provider is asked for its model list.
const catalogueTTL = 10 * time.Minute

type providerModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int64  `json:"context_window"`
	Effort        *struct {
		SupportedLevels []string `json:"supported_levels"`
	} `json:"effort"`
}

// fallbackModels is used before the first fetch and whenever the provider
// cannot be reached, so the settings screen always has a usable choice.
func fallbackModels() []AIModel {
	return []AIModel{
		{
			ID:            "deepseek-flash",
			Name:          "DeepSeek V4.1 Flash",
			ContextWindow: 1_048_576,
			EffortLevels:  EffortLevels,
		},
		{
			ID:            "deepseek-v4-pro",
			Name:          "DeepSeek V4 Pro",
			ContextWindow: 1_048_576,
			EffortLevels:  EffortLevels,
		},
	}
}

// catalogue returns the provider's model list for one key, cached for a short
// interval. The cache is keyed by the key digest so two accounts never share an
// unauthorized result.
func (s *Service) catalogue(ctx context.Context, apiKey string) []AIModel {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fallbackModels()
	}

	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(apiKey)))

	s.catalogueMu.Lock()
	defer s.catalogueMu.Unlock()

	if entry, ok := s.catalogueCache[digest]; ok && time.Since(entry.at) < catalogueTTL {
		return entry.models
	}

	models, err := fetchModels(ctx, s.client, s.cfg, apiKey)
	if err != nil || len(models) == 0 {
		if err != nil {
			s.log.Warn("model catalogue fetch failed", "error", err)
		}
		models = fallbackModels()
	}

	if s.catalogueCache == nil {
		s.catalogueCache = make(map[string]catalogueEntry)
	}
	s.catalogueCache[digest] = catalogueEntry{models: models, at: time.Now()}

	return models
}

// modelKnown reports whether the id may be stored. Legacy aliases stay allowed
// so rows written before the catalogue change keep working.
func (s *Service) modelKnown(ctx context.Context, apiKey, id string) bool {
	if contains(LegacyModels, id) {
		return true
	}

	for _, model := range s.catalogue(ctx, apiKey) {
		if model.ID == id {
			return true
		}
	}

	return false
}

func fetchModels(ctx context.Context, client *http.Client, cfg *config.Config, apiKey string) ([]AIModel, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("DeepSeek API key is not configured")
	}

	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, cfg.DeepSeekBaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
	}()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", response.StatusCode)
	}

	var payload struct {
		Data []providerModel `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}

	models := make([]AIModel, 0, len(payload.Data))
	for _, entry := range payload.Data {
		if entry.ID == "" {
			continue
		}

		name := entry.Name
		if name == "" {
			name = entry.ID
		}

		levels := make([]string, 0, len(EffortLevels))
		if entry.Effort != nil {
			levels = append(levels, entry.Effort.SupportedLevels...)
		}

		models = append(models, AIModel{
			ID:            entry.ID,
			Name:          name,
			ContextWindow: entry.ContextWindow,
			EffortLevels:  levels,
		})
	}

	return models, nil
}
