package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"morrow-api/internal/module/project"
	"morrow-api/internal/module/settings"
	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/wiretime"
)

// maxActions caps how many tasks one reply may create.
const maxActions = 5

// createTaskTool is the only write the assistant may perform.
var createTaskTool = settings.Tool{
	Type: "function",
	Function: settings.ToolFunction{
		Name:        "create_task",
		Description: "Create one task immediately. Call once per task; the task is saved before you reply.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":       map[string]any{"type": "string", "description": "Task title"},
				"project":     map[string]any{"type": "string", "description": "Project id, key, or name; omit to leave the task unfiled"},
				"status":      map[string]any{"type": "string", "enum": []string{"todo", "in_progress", "done"}},
				"priority":    map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
				"dueDate":     map[string]any{"type": "string", "description": "YYYY-MM-DD"},
				"description": map[string]any{"type": "string"},
			},
			"required": []string{"title"},
		},
	},
}

type taskArguments struct {
	Title       string `json:"title"`
	Project     string `json:"project"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	DueDate     string `json:"dueDate"`
	Description string `json:"description"`
}

type taskProposal struct {
	Title       string
	Status      string
	Priority    string
	DueDate     *wiretime.Date
	ProjectID   *string
	ProjectName *string
	Description *string
}

// buildProposals validates the model's tool calls against the workspace. The
// project is resolved against the user's own projects; unknown names drop the
// call rather than creating a stray task.
func buildProposals(calls []settings.ToolCall, projects []project.Project) []taskProposal {
	proposals := make([]taskProposal, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))

	for _, call := range calls {
		if call.Name != "create_task" || len(proposals) == maxActions {
			continue
		}

		var args taskArguments
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			continue
		}

		title := strings.TrimSpace(args.Title)
		if title == "" {
			continue
		}

		status := args.Status
		if !oneOf(status, "todo", "in_progress", "done") {
			status = "todo"
		}

		priority := args.Priority
		if !oneOf(priority, "low", "medium", "high") {
			priority = "medium"
		}

		var dueDate *wiretime.Date
		if args.DueDate != "" {
			if parsed, err := wiretime.ParseDate(args.DueDate); err == nil {
				dueDate = &parsed
			}
		}

		projectID, projectName := resolveProject(args.Project, projects)
		if args.Project != "" && projectID == "" {
			// The model named a project we cannot match; do not guess.
			continue
		}

		proposal := taskProposal{
			Title:    title,
			Status:   status,
			Priority: priority,
			DueDate:  dueDate,
		}
		if projectID != "" {
			proposal.ProjectID = &projectID
			proposal.ProjectName = &projectName
		}
		if description := strings.TrimSpace(args.Description); description != "" {
			proposal.Description = &description
		}

		key := dedupeKey(title, proposal.ProjectID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		proposals = append(proposals, proposal)
	}

	return proposals
}

// executeProposals creates the validated tasks, skipping titles the workspace
// already holds in the same project so a repeated request is not duplicated.
func (s *Service) executeProposals(
	ctx context.Context,
	userID string,
	proposals []taskProposal,
	existing []task.Task,
) []task.Task {
	seen := make(map[string]struct{}, len(existing))
	for _, entry := range existing {
		seen[dedupeKey(entry.Title, entry.ProjectID)] = struct{}{}
	}

	created := make([]task.Task, 0, len(proposals))
	for _, proposal := range proposals {
		if len(created) == maxActions {
			break
		}

		key := dedupeKey(proposal.Title, proposal.ProjectID)
		if _, exists := seen[key]; exists {
			continue
		}

		status, priority := proposal.Status, proposal.Priority
		input := task.New{
			Title:       proposal.Title,
			Description: proposal.Description,
			Status:      &status,
			Priority:    &priority,
			ProjectID:   proposal.ProjectID,
		}
		if proposal.DueDate != nil {
			dueDate := proposal.DueDate.String()
			input.DueDate = &dueDate
		}

		entry, err := s.creator.Create(ctx, userID, input)
		if err != nil {
			s.log.Warn("assistant task creation failed", "error", err)
			continue
		}

		seen[key] = struct{}{}
		created = append(created, *entry)
	}

	return created
}

// resolveProject matches an id, key, or name (case-insensitively).
func resolveProject(value string, projects []project.Project) (string, string) {
	needle := strings.TrimSpace(value)
	if needle == "" {
		return "", ""
	}

	for _, entry := range projects {
		if entry.ID == needle {
			return entry.ID, entry.Name
		}
	}
	for _, entry := range projects {
		if strings.EqualFold(entry.Key, needle) {
			return entry.ID, entry.Name
		}
	}
	for _, entry := range projects {
		if strings.EqualFold(strings.TrimSpace(entry.Name), needle) {
			return entry.ID, entry.Name
		}
	}

	return "", ""
}

// summarizeCreated is used when the model only calls tools and returns no text.
func summarizeCreated(created []task.Task) string {
	parts := make([]string, 0, len(created))
	for _, entry := range created {
		parts = append(parts, entry.Key+" "+entry.Title)
	}

	noun := "tasks"
	if len(created) == 1 {
		noun = "task"
	}

	return fmt.Sprintf("Added %d %s: %s", len(created), noun, strings.Join(parts, ", "))
}

func dedupeKey(title string, projectID *string) string {
	id := ""
	if projectID != nil {
		id = *projectID
	}

	return strings.ToLower(strings.TrimSpace(title)) + "|" + id
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
