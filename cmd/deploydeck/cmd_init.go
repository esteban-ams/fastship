package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

var (
	initService       string
	initComposeFile   string
	initServiceName   string
	initWorkingDir    string
	initMode          string
	initHealthURL     string
	initBranch        string
	initRepo          string
	initWebhookSecret string
	initPublicURL     string
	initCI            string
	initYes           bool
	initForce         bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a config.yaml for a new service",
	Long: `Interactively generate config.yaml with a random webhook secret and a
single service entry, then print a ready-to-paste CI/CD snippet.

Pass --yes with the required flags (--service, --compose-file) to skip the
prompts and run non-interactively (e.g. from a setup script).`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVar(&initService, "service", "", "Service key (also used as the service name unless --service-name is set)")
	initCmd.Flags().StringVar(&initComposeFile, "compose-file", "", "Path to the docker-compose.yml for this service")
	initCmd.Flags().StringVar(&initServiceName, "service-name", "", "Service name inside the compose file (default: same as --service)")
	initCmd.Flags().StringVar(&initWorkingDir, "working-dir", "", "Working directory for compose commands (default: directory of --compose-file)")
	initCmd.Flags().StringVar(&initMode, "mode", "pull", `Deploy mode: "pull" (pre-built image) or "build" (clone + build on server)`)
	initCmd.Flags().StringVar(&initHealthURL, "health-url", "", "Health check URL (leave empty to disable health checks)")
	initCmd.Flags().StringVar(&initBranch, "branch", "main", "Branch to deploy on push (build mode only)")
	initCmd.Flags().StringVar(&initRepo, "repo", "", "Fallback git clone URL (build mode only)")
	initCmd.Flags().StringVar(&initWebhookSecret, "webhook-secret", "", "Webhook secret to use (default: randomly generated)")
	initCmd.Flags().StringVar(&initPublicURL, "public-url", "", "Public URL DeployDeck will be reachable at, e.g. https://deploy.example.com (used only in the printed CI snippet)")
	initCmd.Flags().StringVar(&initCI, "ci", "github", `CI snippet to print: "github", "gitlab", or "none"`)
	initCmd.Flags().BoolVarP(&initYes, "yes", "y", false, "Skip interactive prompts; require --service and --compose-file")
	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite the config file if it already exists")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(configPath); err == nil && !initForce {
		return fmt.Errorf("%s already exists — pass --force to overwrite, or --config to write elsewhere", configPath)
	}

	reader := bufio.NewReader(os.Stdin)

	if !initYes {
		initService = promptString(reader, "Service key (e.g. myapp)", initService)
		initComposeFile = promptString(reader, "Path to docker-compose.yml", initComposeFile)
		if initServiceName == "" {
			initServiceName = initService
		}
		initServiceName = promptString(reader, "Service name inside the compose file", initServiceName)
		initMode = promptString(reader, `Deploy mode ("pull" or "build")`, initMode)
		initHealthURL = promptString(reader, "Health check URL (blank to disable)", initHealthURL)
		if initMode == "build" {
			initBranch = promptString(reader, "Branch to deploy on push", initBranch)
			initRepo = promptString(reader, "Git clone URL (fallback if the webhook payload omits it)", initRepo)
		}
	}

	if initService == "" || initComposeFile == "" {
		return fmt.Errorf("--service and --compose-file are required (use --yes with both flags for non-interactive use)")
	}
	if initServiceName == "" {
		initServiceName = initService
	}
	if initMode != "pull" && initMode != "build" {
		return fmt.Errorf(`--mode must be "pull" or "build", got %q`, initMode)
	}

	secret := initWebhookSecret
	if secret == "" {
		var err error
		secret, err = generateSecret()
		if err != nil {
			return fmt.Errorf("generate webhook secret: %w", err)
		}
	}

	workingDir := initWorkingDir
	if workingDir == "" {
		workingDir = composeFileDir(initComposeFile)
	}

	data := initTemplateData{
		Secret:      secret,
		ServiceKey:  initService,
		ComposeFile: initComposeFile,
		ServiceName: initServiceName,
		WorkingDir:  workingDir,
		Mode:        initMode,
		Branch:      initBranch,
		Repo:        initRepo,
		HealthURL:   initHealthURL,
	}

	yamlText, err := renderConfigYAML(data)
	if err != nil {
		return fmt.Errorf("render config: %w", err)
	}

	if err := os.WriteFile(configPath, []byte(yamlText), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}

	fmt.Printf("\nWrote %s (service %q, mode %s)\n", configPath, initService, initMode)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. deploydeck doctor          # check Docker/Compose/Git and validate the config")
	fmt.Printf("  2. deploydeck --config %s   # start the server\n", configPath)

	if snippet := renderCISnippet(initCI, data, initPublicURL); snippet != "" {
		fmt.Println(snippet)
	}

	if initWebhookSecret == "" {
		fmt.Printf("\nGenerated webhook secret (store it, e.g. as a CI secret named DEPLOYDECK_SECRET):\n  %s\n", secret)
	}

	return nil
}

// generateSecret returns a random 64-character hex string, matching the
// `openssl rand -hex 32` convention used throughout the docs.
func generateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// composeFileDir returns the directory portion of a compose file path,
// falling back to "." for bare filenames.
func composeFileDir(composeFile string) string {
	if i := strings.LastIndex(composeFile, "/"); i >= 0 {
		return composeFile[:i]
	}
	return "."
}

// promptString prints label with the current default and reads a line from
// reader, returning the typed value or def when the line is blank.
func promptString(reader *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

// initTemplateData feeds the config.yaml template and the CI snippet.
type initTemplateData struct {
	Secret      string
	ServiceKey  string
	ComposeFile string
	ServiceName string
	WorkingDir  string
	Mode        string
	Branch      string
	Repo        string
	HealthURL   string
}

var configYAMLTemplate = template.Must(template.New("config").Parse(`# Generated by "deploydeck init". See config.example.yaml for every option.
server:
  port: 9000
  host: "0.0.0.0"

auth:
  webhook_secret: "{{ .Secret }}"

rate_limit:
  enabled: true
  requests_per_minute: 10
  burst_size: 5

dashboard:
  enabled: false
  username: "admin"
  password: "change-me"

logging:
  level: "info"
  format: "text"

services:
  {{ .ServiceKey }}:
    compose_file: "{{ .ComposeFile }}"
    service_name: "{{ .ServiceName }}"
    working_dir: "{{ .WorkingDir }}"
{{- if eq .Mode "build" }}
    mode: "build"
    branch: "{{ .Branch }}"
{{- if .Repo }}
    repo: "{{ .Repo }}"
{{- end }}
{{- end }}
{{- if .HealthURL }}
    health_check:
      enabled: true
      url: "{{ .HealthURL }}"
      timeout: 30s
      interval: 2s
      retries: 10
{{- end }}
    rollback:
      enabled: true
      keep_images: 3
`))

// renderConfigYAML renders a ready-to-run config.yaml for a single service.
func renderConfigYAML(data initTemplateData) (string, error) {
	var buf strings.Builder
	if err := configYAMLTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderCISnippet returns a ready-to-paste CI/CD snippet for the requested
// provider, or the build-mode webhook instructions when data.Mode is
// "build". Returns "" for ci == "none". Plain fmt.Sprintf (not
// text/template) because the snippets themselves contain literal "{{ }}"
// GitHub Actions expression syntax.
func renderCISnippet(ci string, data initTemplateData, publicURL string) string {
	if ci == "none" {
		return ""
	}
	url := publicURL
	if url == "" {
		url = "https://deploy.yourdomain.com"
	}

	if data.Mode == "build" {
		return fmt.Sprintf(`
Build mode: no CI step needed. Instead, add a webhook in your git host:
  Payload URL : %s/api/deploy/%s
  Content type: application/json
  Secret      : the webhook secret printed below
  Events      : push only
Pushes to %q will then clone, build, and deploy automatically.
`, url, data.ServiceKey, data.Branch)
	}

	if ci == "gitlab" {
		return fmt.Sprintf(`
GitLab CI — add this job after building/pushing your image
(also add DEPLOYDECK_SECRET as a masked CI/CD variable):

deploy:
  script:
    - |
      curl -X POST %s/api/deploy/%s \
        -H "X-GitLab-Token: $DEPLOYDECK_SECRET" \
        -H "Content-Type: application/json" \
        -d '{"image": "registry.gitlab.com/you/%s:latest"}'
`, url, data.ServiceKey, data.ServiceKey)
	}

	return fmt.Sprintf(`
GitHub Actions — add this step after building/pushing your image
(also add DEPLOYDECK_SECRET as a repository secret):

  - name: Deploy
    run: |
      curl -X POST %s/api/deploy/%s \
        -H "X-DeployDeck-Secret: ${{ secrets.DEPLOYDECK_SECRET }}" \
        -H "Content-Type: application/json" \
        -d '{"image": "ghcr.io/you/%s:latest"}'
`, url, data.ServiceKey, data.ServiceKey)
}
