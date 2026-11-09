package apitest

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"morrow-api/internal/config"
)

type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
	deletes int
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string][]byte{}, types: map[string]string{}}
}

func (f *fakeS3) server(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/")
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			f.objects[key] = body
			f.types[key] = r.Header.Get("content-type")
			w.WriteHeader(http.StatusOK)
		case http.MethodHead:
			body, ok := f.objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`<Error><Code>NotFound</Code><Message>missing</Message></Error>`))
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Header().Set("Content-Type", f.types[key])
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			body, ok := f.objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`))
				return
			}
			prefix := body
			if len(prefix) > 512 {
				prefix = prefix[:512]
			}
			w.Header().Set("Content-Type", f.types[key])
			w.Header().Set("Content-Length", strconv.Itoa(len(prefix)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(prefix)
		case http.MethodDelete:
			delete(f.objects, key)
			delete(f.types, key)
			f.deletes++
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
}

func attachmentConfig(cfg *config.Config, endpoint string) {
	cfg.S3Endpoint = endpoint
	cfg.S3PublicEndpoint = endpoint
	cfg.S3Region = "us-east-1"
	cfg.S3Bucket = "test-bucket"
	cfg.S3AccessKeyID = "test"
	cfg.S3SecretAccessKey = "secret"
	cfg.S3ForcePathStyle = true
	cfg.AttachmentURLTTLSeconds = 900
}

func TestAttachmentLifecycle(t *testing.T) {
	fake := newFakeS3()
	server := fake.server(t)
	defer server.Close()

	app := NewWithConfig(t, func(cfg *config.Config) {
		attachmentConfig(cfg, server.URL)
	})

	status, created := app.Post(t, "/api/v1/tasks", `{"title":"Attach here"}`)
	if status != http.StatusCreated {
		t.Fatalf("create task = %d %v", status, created)
	}
	taskID, _ := created["id"].(string)

	status, config := app.Get(t, "/api/v1/attachments/config")
	if status != http.StatusOK || config["enabled"] != true {
		t.Fatalf("attachment config = %d %v", status, config)
	}

	pdf := []byte("%PDF-1.4\nattachment body")
	status, ticket := app.Post(t, "/api/v1/attachments",
		`{"entityType":"task","entityId":"`+taskID+`","filename":"brief.pdf","size":`+strconv.Itoa(len(pdf))+`}`)
	if status != http.StatusCreated {
		t.Fatalf("prepare = %d %v", status, ticket)
	}
	attachment, _ := ticket["attachment"].(map[string]any)
	attachmentID, _ := attachment["id"].(string)
	uploadURL, _ := ticket["uploadUrl"].(string)
	if attachmentID == "" || uploadURL == "" {
		t.Fatalf("prepare ticket = %v", ticket)
	}

	request, err := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader(pdf))
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	headers, _ := ticket["headers"].(map[string]any)
	for name, value := range headers {
		if strings.EqualFold(name, "host") {
			continue
		}
		if values, ok := value.([]any); ok && len(values) > 0 {
			request.Header.Set(name, values[0].(string))
		}
	}
	if request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/pdf")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", response.StatusCode)
	}

	status, completed := app.Post(t, "/api/v1/attachments/"+attachmentID+"/complete", "")
	if status != http.StatusOK || completed["size"] != float64(len(pdf)) {
		t.Fatalf("complete = %d %v", status, completed)
	}

	status, list := app.GetList(t, "/api/v1/attachments?entityType=task&entityId="+taskID)
	if status != http.StatusOK || len(list) != 1 {
		t.Fatalf("list = %d %v", status, list)
	}

	status, signed := app.Get(t, "/api/v1/attachments/"+attachmentID+"/url?inline=true")
	if status != http.StatusOK {
		t.Fatalf("url = %d %v", status, signed)
	}
	downloadURL, _ := signed["url"].(string)
	download, err := http.Get(downloadURL)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	body, _ := io.ReadAll(download.Body)
	_ = download.Body.Close()
	if !bytes.Equal(body, pdf) {
		t.Errorf("download body = %q", body)
	}

	if status := app.Status(t, http.MethodDelete, "/api/v1/attachments/"+attachmentID, app.Token, ""); status != http.StatusNoContent {
		t.Fatalf("delete = %d", status)
	}
	status, list = app.GetList(t, "/api/v1/attachments?entityType=task&entityId="+taskID)
	if status != http.StatusOK || len(list) != 0 {
		t.Fatalf("list after delete = %d %v", status, list)
	}
}

func TestAttachmentValidationAndCleanup(t *testing.T) {
	fake := newFakeS3()
	server := fake.server(t)
	defer server.Close()

	app := NewWithConfig(t, func(cfg *config.Config) {
		attachmentConfig(cfg, server.URL)
	})

	status, task := app.Post(t, "/api/v1/tasks", `{"title":"Cleanup"}`)
	if status != http.StatusCreated {
		t.Fatalf("create task = %d %v", status, task)
	}
	taskID, _ := task["id"].(string)

	if status, body := app.Post(t, "/api/v1/attachments",
		`{"entityType":"task","entityId":"`+taskID+`","filename":"run.sh","size":12}`); status != http.StatusBadRequest {
		t.Fatalf("unsupported type = %d %v", status, body)
	}

	status, ticket := app.Post(t, "/api/v1/attachments",
		`{"entityType":"task","entityId":"`+taskID+`","filename":"fake.pdf","size":12}`)
	if status != http.StatusCreated {
		t.Fatalf("prepare = %d %v", status, ticket)
	}
	attachment, _ := ticket["attachment"].(map[string]any)
	attachmentID, _ := attachment["id"].(string)
	uploadURL, _ := ticket["uploadUrl"].(string)
	request, _ := http.NewRequest(http.MethodPut, uploadURL, strings.NewReader("not a pdf"))
	request.Header.Set("Content-Type", "application/pdf")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	_ = response.Body.Close()

	if status, body := app.Post(t, "/api/v1/attachments/"+attachmentID+"/complete", ""); status != http.StatusBadRequest {
		t.Fatalf("magic validation = %d %v", status, body)
	}

	status, ticket = app.Post(t, "/api/v1/attachments",
		`{"entityType":"task","entityId":"`+taskID+`","filename":"report.docx","size":8}`)
	if status != http.StatusCreated {
		t.Fatalf("prepare office = %d %v", status, ticket)
	}
	attachment, _ = ticket["attachment"].(map[string]any)
	attachmentID, _ = attachment["id"].(string)
	uploadURL, _ = ticket["uploadUrl"].(string)
	request, _ = http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader([]byte("PK\x03\x04test")))
	request.Header.Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("upload office: %v", err)
	}
	_ = response.Body.Close()
	if status, body := app.Post(t, "/api/v1/attachments/"+attachmentID+"/complete", ""); status != http.StatusOK {
		t.Fatalf("complete office = %d %v", status, body)
	}

	before := fake.deletes
	if status := app.Status(t, http.MethodDelete, "/api/v1/tasks/"+taskID, app.Token, ""); status != http.StatusNoContent {
		t.Fatalf("delete task = %d", status)
	}
	if fake.deletes <= before {
		t.Errorf("task delete did not remove the stored object")
	}
	var count int64
	if err := app.DB.Table("attachments").Where("entity_id = ?", taskID).Count(&count).Error; err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	if count != 0 {
		t.Errorf("attachments after task delete = %d", count)
	}
}

func TestAttachmentProjectAndNoteTargets(t *testing.T) {
	fake := newFakeS3()
	server := fake.server(t)
	defer server.Close()

	app := NewWithConfig(t, func(cfg *config.Config) {
		attachmentConfig(cfg, server.URL)
	})

	status, project := app.Post(t, "/api/v1/projects", `{"name":"Attach project"}`)
	if status != http.StatusCreated {
		t.Fatalf("create project = %d %v", status, project)
	}
	projectID, _ := project["id"].(string)

	status, note := app.Post(t, "/api/v1/notes", `{"title":"Attach note"}`)
	if status != http.StatusCreated {
		t.Fatalf("create note = %d %v", status, note)
	}
	noteID, _ := note["id"].(string)

	for _, target := range []struct {
		entityType string
		entityID   string
	}{
		{"project", projectID},
		{"note", noteID},
	} {
		status, list := app.GetList(t, "/api/v1/attachments?entityType="+target.entityType+"&entityId="+target.entityID)
		if status != http.StatusOK || len(list) != 0 {
			t.Fatalf("%s list = %d %v", target.entityType, status, list)
		}
		status, ticket := app.Post(t, "/api/v1/attachments",
			`{"entityType":"`+target.entityType+`","entityId":"`+target.entityID+`","filename":"photo.png","size":8}`)
		if status != http.StatusCreated {
			t.Fatalf("%s prepare = %d %v", target.entityType, status, ticket)
		}
	}

	if status := app.Status(t, http.MethodDelete, "/api/v1/projects/"+projectID, app.Token, ""); status != http.StatusNoContent {
		t.Fatalf("delete project = %d", status)
	}
	if status := app.Status(t, http.MethodDelete, "/api/v1/notes/"+noteID, app.Token, ""); status != http.StatusNoContent {
		t.Fatalf("delete note = %d", status)
	}

	var count int64
	if err := app.DB.Table("attachments").Count(&count).Error; err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	if count != 0 {
		t.Errorf("attachments after entity deletes = %d", count)
	}
}
