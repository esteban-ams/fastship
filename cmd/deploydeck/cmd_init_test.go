package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/esteban-ams/deploydeck/internal/config"
)

func TestGenerateSecret(t *testing.T) {
	a, err := generateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := generateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(a) != 64 {
		t.Errorf("expected 64 hex chars (32 bytes), got %d: %q", len(a), a)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(a) {
		t.Errorf("expected lowercase hex string, got %q", a)
	}
	if a == b {
		t.Error("two calls returned the same secret — not random")
	}
}

func TestComposeFileDir(t *testing.T) {
	cases := map[string]string{
		"/opt/apps/docker-compose.yml": "/opt/apps",
		"repo/docker-compose.yml":      "repo",
		"docker-compose.yml":           ".",
	}
	for in, want := range cases {
		if got := composeFileDir(in); got != want {
			t.Errorf("composeFileDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// loadRendered writes yamlText to a temp config.yaml and loads it through
// the real config package, proving "deploydeck init" output is always
// valid, loadable configuration — not just well-formed YAML.
func loadRendered(t *testing.T, yamlText string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlText), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("generated config.yaml failed to load: %v\n---\n%s", err, yamlText)
	}
	return cfg
}

func TestRenderConfigYAML_PullMode(t *testing.T) {
	data := initTemplateData{
		Secret:      "abc123",
		ServiceKey:  "myapp",
		ComposeFile: "/opt/apps/docker-compose.yml",
		ServiceName: "myapp",
		WorkingDir:  "/opt/apps",
		Mode:        "pull",
		HealthURL:   "http://localhost:8080/health",
	}

	yamlText, err := renderConfigYAML(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := loadRendered(t, yamlText)

	if cfg.Auth.WebhookSecret != "abc123" {
		t.Errorf("webhook_secret = %q, want %q", cfg.Auth.WebhookSecret, "abc123")
	}

	svc, ok := cfg.Services["myapp"]
	if !ok {
		t.Fatalf("service %q missing from parsed config", "myapp")
	}
	if svc.Mode != config.DeployModePull {
		t.Errorf("mode = %q, want %q (pull is the implicit default)", svc.Mode, config.DeployModePull)
	}
	if svc.ComposeFile != data.ComposeFile {
		t.Errorf("compose_file = %q, want %q", svc.ComposeFile, data.ComposeFile)
	}
	if !svc.HealthCheck.Enabled || svc.HealthCheck.URL != data.HealthURL {
		t.Errorf("health_check = %+v, want enabled with url %q", svc.HealthCheck, data.HealthURL)
	}
	if !svc.Rollback.Enabled {
		t.Error("expected rollback enabled by default")
	}
}

func TestRenderConfigYAML_BuildMode(t *testing.T) {
	data := initTemplateData{
		Secret:      "s3cr3t",
		ServiceKey:  "api",
		ComposeFile: "repo/docker-compose.yml",
		ServiceName: "api",
		WorkingDir:  "repo",
		Mode:        "build",
		Branch:      "develop",
		Repo:        "https://github.com/x/y.git",
	}

	yamlText, err := renderConfigYAML(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := loadRendered(t, yamlText)

	svc, ok := cfg.Services["api"]
	if !ok {
		t.Fatalf("service %q missing from parsed config", "api")
	}
	if svc.Mode != config.DeployModeBuild {
		t.Errorf("mode = %q, want %q", svc.Mode, config.DeployModeBuild)
	}
	if svc.Branch != "develop" {
		t.Errorf("branch = %q, want %q", svc.Branch, "develop")
	}
	if svc.Repo != data.Repo {
		t.Errorf("repo = %q, want %q", svc.Repo, data.Repo)
	}
	if svc.HealthCheck.Enabled {
		t.Error("expected health check disabled when HealthURL is empty")
	}
}

func TestRenderCISnippet(t *testing.T) {
	pull := initTemplateData{ServiceKey: "myapp", Mode: "pull"}
	build := initTemplateData{ServiceKey: "api", Mode: "build", Branch: "develop"}

	if got := renderCISnippet("none", pull, ""); got != "" {
		t.Errorf(`ci="none" should render nothing, got %q`, got)
	}

	gh := renderCISnippet("github", pull, "")
	if !strings.Contains(gh, "https://deploy.yourdomain.com/api/deploy/myapp") {
		t.Errorf("github snippet missing default public URL + endpoint: %s", gh)
	}
	if !strings.Contains(gh, "X-DeployDeck-Secret") {
		t.Errorf("github snippet missing auth header: %s", gh)
	}

	gl := renderCISnippet("gitlab", pull, "https://deploy.example.com")
	if !strings.Contains(gl, "https://deploy.example.com/api/deploy/myapp") {
		t.Errorf("gitlab snippet missing custom public URL: %s", gl)
	}
	if !strings.Contains(gl, "X-GitLab-Token") {
		t.Errorf("gitlab snippet missing auth header: %s", gl)
	}

	// Build mode always prints webhook setup instructions, regardless of --ci.
	bm := renderCISnippet("github", build, "")
	if !strings.Contains(bm, "add a webhook") || strings.Contains(bm, "GitHub Actions") {
		t.Errorf("build mode should print webhook instructions, not a CI job: %s", bm)
	}
	if !strings.Contains(bm, `"develop"`) {
		t.Errorf("build mode snippet should mention the configured branch: %s", bm)
	}
}
