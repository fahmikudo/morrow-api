package assistant

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"morrow-api/internal/config"
	"morrow-api/internal/module/area"
	"morrow-api/internal/module/event"
	"morrow-api/internal/module/habit"
	"morrow-api/internal/module/inbox"
	"morrow-api/internal/module/note"
	"morrow-api/internal/module/project"
	"morrow-api/internal/module/settings"
	"morrow-api/internal/module/task"
	"morrow-api/internal/platform/htmltext"
	"morrow-api/internal/platform/httpx"
	"morrow-api/internal/platform/wiretime"
)

var monthNames = [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

type (
	taskLister interface {
		List(ctx context.Context, userID string) ([]task.Task, error)
	}
	projectLister interface {
		List(ctx context.Context, userID string) ([]project.Project, error)
	}
	noteLister interface {
		List(ctx context.Context, userID string) ([]note.Note, error)
	}
	inboxLister interface {
		ListActive(ctx context.Context, userID string) ([]inbox.Item, error)
	}
	eventLister interface {
		List(ctx context.Context, userID string) ([]event.Event, error)
	}
	habitLister interface {
		ListActive(ctx context.Context, userID string) ([]habit.Habit, error)
	}
	areaLister interface {
		List(ctx context.Context, userID string) ([]area.Area, error)
	}
	taskCreator interface {
		Create(ctx context.Context, userID string, input task.New) (*task.Task, error)
	}
	aiProvider interface {
		AI(ctx context.Context, userID string) (settings.AISettings, *settings.AIRow, error)
	}
)

type replyContext struct {
	tasks    []task.Task
	projects []project.Project
	notes    []note.Note
	inbox    []inbox.Item
	events   []event.Event
	habits   []habit.Habit
	areas    []area.Area
}

// Service answers assistant questions with derived replies, escalating to
// DeepSeek only when it is enabled and configured.
type Service struct {
	cfg      *config.Config
	log      *slog.Logger
	client   *http.Client
	tasks    taskLister
	projects projectLister
	notes    noteLister
	inbox    inboxLister
	events   eventLister
	habits   habitLister
	areas    areaLister
	creator  taskCreator
	settings aiProvider
}

// NewService wires the assistant service.
func NewService(
	cfg *config.Config,
	log *slog.Logger,
	client *http.Client,
	tasks taskLister,
	projects projectLister,
	notes noteLister,
	inbox inboxLister,
	events eventLister,
	habits habitLister,
	areas areaLister,
	creator taskCreator,
	ai aiProvider,
) *Service {
	return &Service{
		cfg:      cfg,
		log:      log,
		client:   client,
		tasks:    tasks,
		projects: projects,
		notes:    notes,
		inbox:    inbox,
		events:   events,
		habits:   habits,
		areas:    areas,
		creator:  creator,
		settings: ai,
	}
}

// Reply builds the derived reply first, then only replaces it when the model
// answers. The response names the items it is grounded in and, for a fallback,
// why the model was not used.
func (s *Service) Reply(ctx context.Context, userID string, request Request) (*Response, error) {
	tasks, err := s.tasks.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	projects, err := s.projects.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	notes, err := s.notes.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	inboxItems, err := s.inbox.ListActive(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	events, err := s.events.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	habits, err := s.habits.ListActive(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}
	areas, err := s.areas.List(ctx, userID)
	if err != nil {
		return nil, httpx.Map(err, s.log)
	}

	now := wiretime.Now().Time()
	today := DateOf(now)
	if request.Today != nil {
		today = *request.Today
	}

	current := replyContext{
		tasks:    tasks,
		projects: projects,
		notes:    notes,
		inbox:    inboxItems,
		events:   events,
		habits:   habits,
		areas:    areas,
	}
	promptText, catalogue := buildPrompt(current, today)
	fallback := generateReply(request.Message, current, now, today)

	_, record, err := s.settings.AI(ctx, userID)
	if err != nil {
		return nil, err
	}

	reply := fallback
	provider := "fallback"
	reason := ""
	sources := []Source{}
	created := []task.Task{}

	switch {
	case !record.Enabled:
		reason = ReasonAIDisabled
	case !record.KeyConfigured():
		reason = ReasonKeyMissing
	default:
		completion, err := settings.Complete(
			ctx, s.client, s.cfg, *record, record.APIKeyValue(), promptText, request.Message,
			[]settings.Tool{createTaskTool},
		)
		switch {
		case err != nil:
			s.log.Warn("assistant completion failed", "error", err)
			reason = ReasonProviderError
		case strings.TrimSpace(completion.Content) == "" && len(completion.ToolCalls) == 0:
			reason = ReasonEmptyCompletion
		default:
			proposals := buildProposals(completion.ToolCalls, projects)
			created = s.executeProposals(ctx, userID, proposals, tasks)
			cleaned, ids := splitCitationLine(completion.Content)
			reply = cleaned
			if reply == "" && len(created) > 0 {
				reply = summarizeCreated(created)
			}
			provider = record.Provider
			sources = sourcesFromIDs(ids, catalogue)
			if len(sources) == 0 {
				sources = mentionedSources(reply, catalogue)
			}
		}
	}

	if len(sources) == 0 {
		// Derived replies name the items they used; cite what the text mentions.
		sources = mentionedSources(reply, catalogue)
	}
	if sources == nil {
		sources = []Source{}
	}
	if created == nil {
		created = []task.Task{}
	}

	return &Response{Reply: reply, Provider: provider, Sources: sources, Created: created, Reason: reason}, nil
}

// DateOf keeps only the UTC calendar date of an instant.
func DateOf(value time.Time) wiretime.Date {
	utc := value.UTC()
	return wiretime.Date(time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC))
}

func priorityRank(priority string) int32 {
	switch priority {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	default:
		return 0
	}
}

func formatDate(date wiretime.Date) string {
	return monthNames[int(date.Time().Month())-1] + " " + strconv.Itoa(date.Time().Day())
}

func dueLabel(due, today wiretime.Date) string {
	diff := int(due.Time().Sub(today.Time()).Hours() / 24)
	switch {
	case diff < 0:
		return "Overdue by " + strconv.Itoa(-diff) + "d"
	case diff == 0:
		return "Today"
	case diff == 1:
		return "Tomorrow"
	case diff <= 7:
		return "in " + strconv.Itoa(diff) + "d"
	default:
		return formatDate(due)
	}
}

func isOverdue(current task.Task, today wiretime.Date) bool {
	if current.DueDate == nil {
		return false
	}

	return current.Status != "done" && current.DueDate.Time().Before(today.Time())
}

func isDueToday(current task.Task, today wiretime.Date) bool {
	return current.DueDate != nil && current.DueDate.String() == today.String()
}

func projectProgress(projectID string, tasks []task.Task) (done, total, percent int32) {
	parentIDs := make(map[string]struct{})
	for _, current := range tasks {
		if current.ParentID != nil {
			parentIDs[*current.ParentID] = struct{}{}
		}
	}

	leaves := make([]task.Task, 0)
	for _, current := range tasks {
		if current.ProjectID == nil || *current.ProjectID != projectID {
			continue
		}
		if _, isParent := parentIDs[current.ID]; isParent {
			continue
		}
		leaves = append(leaves, current)
	}

	total = int32(len(leaves))
	for _, current := range leaves {
		if current.Status == "done" {
			done++
		}
	}
	if total > 0 {
		percent = int32(math.Round(float64(done) / float64(total) * 100))
	}

	return done, total, percent
}

func focusReply(current replyContext, today wiretime.Date) string {
	projectNames := make(map[string]string, len(current.projects))
	for _, entry := range current.projects {
		projectNames[entry.ID] = entry.Name
	}

	rank := func(entry task.Task) int {
		switch {
		case isOverdue(entry, today):
			return 0
		case isDueToday(entry, today):
			return 1
		case entry.Priority == "high":
			return 2
		default:
			return 3
		}
	}

	candidates := make([]task.Task, 0, len(current.tasks))
	for _, entry := range current.tasks {
		if entry.Status != "done" && rank(entry) < 3 {
			candidates = append(candidates, entry)
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if left, right := rank(candidates[i]), rank(candidates[j]); left != right {
			return left < right
		}
		if left, right := priorityRank(candidates[i].Priority), priorityRank(candidates[j].Priority); left != right {
			return left > right
		}
		return candidates[i].Title < candidates[j].Title
	})
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}

	if len(candidates) == 0 {
		return "Nothing is overdue or due today, so nothing is fighting for your attention. Pick one thing from Upcoming and finish it."
	}

	lines := make([]string, 0, len(candidates))
	for _, entry := range candidates {
		due := "no due date"
		if entry.DueDate != nil {
			due = dueLabel(*entry.DueDate, today)
		}

		line := "• " + entry.Title + " — " + due
		if entry.ProjectID != nil {
			if name, ok := projectNames[*entry.ProjectID]; ok {
				line += " · " + name
			}
		}
		lines = append(lines, line)
	}

	return "Focus on these first:\n" + strings.Join(lines, "\n")
}

func summaryReply(current replyContext, now time.Time, today wiretime.Date) string {
	dueToday := 0
	overdue := 0
	for _, entry := range current.tasks {
		if entry.Status == "done" {
			continue
		}
		if isDueToday(entry, today) {
			dueToday++
		}
		if isOverdue(entry, today) {
			overdue++
		}
	}

	inProgress := 0
	doneThisWeek := 0
	weekAgo := now.Add(-7 * 24 * time.Hour)
	for _, entry := range current.tasks {
		if entry.Status == "in_progress" {
			inProgress++
		}
		if entry.Status == "done" && !entry.UpdatedAt.Time().Before(weekAgo) {
			doneThisWeek++
		}
	}

	inboxCount := len(current.inbox)
	itemWord := "items"
	if inboxCount == 1 {
		itemWord = "item"
	}

	lines := []string{
		"Here is your day in numbers:",
		"• " + strconv.Itoa(dueToday) + " due today · " + strconv.Itoa(overdue) + " overdue",
		"• " + strconv.Itoa(inProgress) + " in progress · " + strconv.Itoa(doneThisWeek) + " completed in the last 7 days",
		"• " + strconv.Itoa(inboxCount) + " " + itemWord + " waiting in the Inbox",
	}

	return strings.Join(lines, "\n")
}

func projectsReply(current replyContext) string {
	active := make([]project.Project, 0, len(current.projects))
	for _, entry := range current.projects {
		if entry.Status == "active" {
			active = append(active, entry)
		}
	}

	if len(active) == 0 {
		return "You have no active projects right now. Create one when a goal needs more than a single task."
	}

	if len(active) > 4 {
		active = active[:4]
	}

	lines := make([]string, 0, len(active))
	for _, entry := range active {
		done, total, percent := projectProgress(entry.ID, current.tasks)
		lines = append(lines, "• "+entry.Name+" — "+strconv.Itoa(int(percent))+"% ("+strconv.Itoa(int(done))+"/"+strconv.Itoa(int(total))+" tasks)")
	}

	return "Active projects:\n" + strings.Join(lines, "\n")
}

func notesReply(current replyContext) string {
	recent := make([]note.Note, len(current.notes))
	copy(recent, current.notes)
	sort.SliceStable(recent, func(i, j int) bool {
		return recent[i].UpdatedAt.Time().After(recent[j].UpdatedAt.Time())
	})
	if len(recent) > 3 {
		recent = recent[:3]
	}

	if len(recent) == 0 {
		return "You have no notes yet. Capture something and it will show up here."
	}

	lines := make([]string, 0, len(recent))
	for _, entry := range recent {
		lines = append(lines, "• "+entry.Title)
	}

	return "Recently updated notes:\n" + strings.Join(lines, "\n")
}

func generateReply(input string, current replyContext, now time.Time, today wiretime.Date) string {
	text := strings.ToLower(input)

	if strings.Contains(text, "focus") || strings.Contains(text, "today") {
		return focusReply(current, today)
	}
	if strings.Contains(text, "summar") || strings.Contains(text, "day") {
		return summaryReply(current, now, today)
	}
	if strings.Contains(text, "project") {
		return projectsReply(current)
	}
	if strings.Contains(text, "note") || strings.Contains(text, "find") {
		return notesReply(current)
	}

	return Fallback
}

type promptTask struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority"`
	DueDate     *string  `json:"dueDate"`
	ProjectID   *string  `json:"projectId"`
	Labels      []string `json:"labels"`
	Description string   `json:"description"`
}

type promptProject struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type promptNote struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updatedAt"`
	Content   string `json:"content"`
}

type promptInbox struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

type promptEvent struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Date      string  `json:"date"`
	StartTime *string `json:"startTime"`
	EndTime   *string `json:"endTime"`
	ProjectID *string `json:"projectId"`
}

type promptHabit struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Cadence   string  `json:"cadence"`
	Target    int32   `json:"target"`
	StartDate string  `json:"startDate"`
	ProjectID *string `json:"projectId"`
}

type promptArea struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type promptContext struct {
	Today    string          `json:"today"`
	Tasks    []promptTask    `json:"tasks"`
	Projects []promptProject `json:"projects"`
	Notes    []promptNote    `json:"notes"`
	Inbox    []promptInbox   `json:"inbox"`
	Events   []promptEvent   `json:"events"`
	Habits   []promptHabit   `json:"habits"`
	Areas    []promptArea    `json:"areas"`
}

// maxSources caps the citation list under a reply.
const maxSources = 6

// buildPrompt renders the grounded context and the catalogue that maps the ids
// inside it back to clickable sources.
func buildPrompt(current replyContext, today wiretime.Date) (string, []Source) {
	catalogue := make([]Source, 0, len(current.tasks)+len(current.notes)+16)

	taskRows := make([]promptTask, 0, len(current.tasks))
	for _, entry := range current.tasks {
		if len(taskRows) == 200 {
			break
		}

		var dueDate *string
		if entry.DueDate != nil {
			value := entry.DueDate.String()
			dueDate = &value
		}

		taskRows = append(taskRows, promptTask{
			ID:          entry.ID,
			Key:         entry.Key,
			Title:       entry.Title,
			Status:      entry.Status,
			Priority:    entry.Priority,
			DueDate:     dueDate,
			ProjectID:   entry.ProjectID,
			Labels:      entry.Labels,
			Description: strings.TrimSpace(htmltext.Truncate(htmltext.StripHTML(deref(entry.Description)), 200)),
		})
		catalogue = append(catalogue, Source{Kind: "task", ID: entry.ID, Title: entry.Title, Href: "/tasks"})
	}

	projectRows := make([]promptProject, 0, len(current.projects))
	for _, entry := range current.projects {
		projectRows = append(projectRows, promptProject{ID: entry.ID, Name: entry.Name, Status: entry.Status})
		catalogue = append(catalogue, Source{Kind: "project", ID: entry.ID, Title: entry.Name, Href: "/projects/" + entry.ID})
	}

	recentNotes := make([]note.Note, len(current.notes))
	copy(recentNotes, current.notes)
	sort.SliceStable(recentNotes, func(i, j int) bool {
		return recentNotes[i].UpdatedAt.Time().After(recentNotes[j].UpdatedAt.Time())
	})
	if len(recentNotes) > 40 {
		recentNotes = recentNotes[:40]
	}

	noteRows := make([]promptNote, 0, len(recentNotes))
	for _, entry := range recentNotes {
		noteRows = append(noteRows, promptNote{
			ID:        entry.ID,
			Title:     entry.Title,
			UpdatedAt: entry.UpdatedAt.Time().Format("2006-01-02T15:04:05.000Z"),
			Content:   strings.TrimSpace(htmltext.Truncate(htmltext.StripHTML(entry.Content), 800)),
		})
		catalogue = append(catalogue, Source{Kind: "note", ID: entry.ID, Title: entry.Title, Href: "/knowledge/" + entry.ID})
	}

	inboxRows := make([]promptInbox, 0, len(current.inbox))
	for _, entry := range current.inbox {
		if len(inboxRows) == 50 {
			break
		}
		inboxRows = append(inboxRows, promptInbox{ID: entry.ID, Title: entry.Title, Type: entry.Type})
		catalogue = append(catalogue, Source{Kind: "inbox", ID: entry.ID, Title: entry.Title, Href: "/inbox"})
	}

	eventRows := make([]promptEvent, 0, len(current.events))
	for _, entry := range current.events {
		if len(eventRows) == 40 {
			break
		}
		eventRows = append(eventRows, promptEvent{
			ID:        entry.ID,
			Title:     entry.Title,
			Date:      entry.Date.String(),
			StartTime: entry.StartTime,
			EndTime:   entry.EndTime,
			ProjectID: entry.ProjectID,
		})
		catalogue = append(catalogue, Source{Kind: "event", ID: entry.ID, Title: entry.Title, Href: "/calendar"})
	}

	habitRows := make([]promptHabit, 0, len(current.habits))
	for _, entry := range current.habits {
		habitRows = append(habitRows, promptHabit{
			ID:        entry.ID,
			Title:     entry.Title,
			Cadence:   entry.Cadence,
			Target:    entry.Target,
			StartDate: entry.StartDate.String(),
			ProjectID: entry.ProjectID,
		})
		catalogue = append(catalogue, Source{Kind: "habit", ID: entry.ID, Title: entry.Title, Href: "/habits"})
	}

	areaRows := make([]promptArea, 0, len(current.areas))
	for _, entry := range current.areas {
		areaRows = append(areaRows, promptArea{
			ID:          entry.ID,
			Name:        entry.Name,
			Description: strings.TrimSpace(htmltext.Truncate(htmltext.StripHTML(deref(entry.Description)), 200)),
		})
		catalogue = append(catalogue, Source{Kind: "area", ID: entry.ID, Title: entry.Name, Href: "/areas/" + entry.ID})
	}

	encoded, err := json.Marshal(promptContext{
		Today:    today.String(),
		Tasks:    taskRows,
		Projects: projectRows,
		Notes:    noteRows,
		Inbox:    inboxRows,
		Events:   eventRows,
		Habits:   habitRows,
		Areas:    areaRows,
	})
	if err != nil {
		encoded = []byte("{}")
	}

	prompt := "You are the assistant inside Morrow, a personal productivity app. " +
		"Answer using only the provided context. Be concise (max 120 words), use plain text bullets with \"•\", " +
		"and never invent items that are not in the context. If the context is empty, say so. " +
		"You can create tasks with the create_task tool: it saves the task immediately, so call it once per task when " +
		"the user asks to add work, then confirm in your reply what you added (keys are shown by the app). " +
		"After the answer, add one final line exactly in the form `Sources: id1, id2` naming the ids you relied on " +
		"(\"Sources: none\" when none apply), using only ids present in the context.\n\nContext (JSON):\n" +
		string(encoded)

	return prompt, catalogue
}

var citationLine = regexp.MustCompile(`(?im)^\s*sources:\s*(.*)$`)

// splitCitationLine removes the model's trailing `Sources:` line and returns
// the cited ids.
func splitCitationLine(reply string) (string, []string) {
	loc := citationLine.FindStringSubmatchIndex(reply)
	if loc == nil {
		return strings.TrimSpace(reply), nil
	}

	return strings.TrimSpace(reply[:loc[0]] + reply[loc[1]:]), parseIDs(reply[loc[2]:loc[3]])
}

func parseIDs(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})

	ids := make([]string, 0, len(fields))
	for _, field := range fields {
		id := strings.Trim(field, "`[]()\"'.")
		if id == "" || strings.EqualFold(id, "none") {
			continue
		}
		ids = append(ids, id)
	}

	return ids
}

func sourcesFromIDs(ids []string, catalogue []Source) []Source {
	if len(ids) == 0 {
		return nil
	}

	byID := make(map[string]Source, len(catalogue))
	for _, source := range catalogue {
		byID[source.ID] = source
	}

	sources := make([]Source, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		source, ok := byID[id]
		if !ok {
			continue
		}
		sources = append(sources, source)
		seen[id] = struct{}{}
		if len(sources) == maxSources {
			break
		}
	}

	if len(sources) == 0 {
		return nil
	}

	return sources
}

// mentionedSources cites the context items whose titles appear in the reply.
// It covers derived replies, which name their items but carry no id list.
func mentionedSources(reply string, catalogue []Source) []Source {
	sources := make([]Source, 0, maxSources)
	seen := make(map[string]struct{}, maxSources)

	for _, source := range catalogue {
		title := strings.TrimSpace(source.Title)
		if len([]rune(title)) < 3 || !strings.Contains(reply, title) {
			continue
		}
		if _, ok := seen[source.ID]; ok {
			continue
		}

		sources = append(sources, source)
		seen[source.ID] = struct{}{}
		if len(sources) == maxSources {
			break
		}
	}

	if len(sources) == 0 {
		return nil
	}

	return sources
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
