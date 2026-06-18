package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v2"
)

func TestGrafanaPrometheusDatasourceProvisioning(t *testing.T) {
	t.Parallel()

	content := readModuleFile(t, "grafana", "provisioning", "datasources", "prometheus.yml")

	var config struct {
		APIVersion  int `yaml:"apiVersion"`
		Datasources []struct {
			Name      string `yaml:"name"`
			UID       string `yaml:"uid"`
			Type      string `yaml:"type"`
			URL       string `yaml:"url"`
			IsDefault bool   `yaml:"isDefault"`
		} `yaml:"datasources"`
	}
	if err := yaml.Unmarshal(content, &config); err != nil {
		t.Fatalf("unmarshal datasource provisioning: %v", err)
	}

	if config.APIVersion != 1 {
		t.Fatalf("expected apiVersion 1, got %d", config.APIVersion)
	}
	if len(config.Datasources) != 1 {
		t.Fatalf("expected exactly one datasource, got %d", len(config.Datasources))
	}

	datasource := config.Datasources[0]
	if datasource.Name != "Prometheus" {
		t.Fatalf("expected datasource name Prometheus, got %q", datasource.Name)
	}
	if datasource.UID != "prometheus" {
		t.Fatalf("expected datasource UID prometheus, got %q", datasource.UID)
	}
	if datasource.Type != "prometheus" {
		t.Fatalf("expected datasource type prometheus, got %q", datasource.Type)
	}
	if datasource.URL != "http://prometheus:9090" {
		t.Fatalf("expected datasource URL http://prometheus:9090, got %q", datasource.URL)
	}
	if !datasource.IsDefault {
		t.Fatal("expected Prometheus datasource to be default")
	}
}

func TestGrafanaDashboardProvisioningReferencesDashboardPath(t *testing.T) {
	t.Parallel()

	content := readModuleFile(t, "grafana", "provisioning", "dashboards", "dashboard.yml")

	var config struct {
		APIVersion int `yaml:"apiVersion"`
		Providers  []struct {
			Name    string `yaml:"name"`
			Type    string `yaml:"type"`
			Options struct {
				Path string `yaml:"path"`
			} `yaml:"options"`
		} `yaml:"providers"`
	}
	if err := yaml.Unmarshal(content, &config); err != nil {
		t.Fatalf("unmarshal dashboard provisioning: %v", err)
	}

	if config.APIVersion != 1 {
		t.Fatalf("expected apiVersion 1, got %d", config.APIVersion)
	}
	if len(config.Providers) != 1 {
		t.Fatalf("expected exactly one provider, got %d", len(config.Providers))
	}

	provider := config.Providers[0]
	if provider.Type != "file" {
		t.Fatalf("expected provider type file, got %q", provider.Type)
	}
	if provider.Options.Path != "/var/lib/grafana/dashboards" {
		t.Fatalf("expected dashboard path /var/lib/grafana/dashboards, got %q", provider.Options.Path)
	}
}

func TestGrafanaDashboardContainsRequiredPanelsAndQueries(t *testing.T) {
	t.Parallel()

	content := readModuleFile(t, "grafana", "dashboards", "payment-webhook-processor.json")

	var dashboard map[string]any
	if err := json.Unmarshal(content, &dashboard); err != nil {
		t.Fatalf("unmarshal dashboard JSON: %v", err)
	}

	panels, ok := dashboard["panels"].([]any)
	if !ok {
		t.Fatal("dashboard panels missing or invalid")
	}

	requiredTitles := []string{
		"Webhook Request Rate",
		"Webhook Success and Error Rate",
		"P95 Webhook Latency",
		"P95 Database Write Latency",
		"Duplicate Event Count",
		"Anomaly Count",
		"Provider Traffic Spike",
		"Payment Event Status Distribution",
		"DB Pool Connections",
		"DB Pool Wait Rate",
		"DB Pool Wait Duration",
	}

	for _, title := range requiredTitles {
		if !dashboardHasPanelTitle(panels, title) {
			t.Fatalf("expected dashboard to contain panel %q", title)
		}
	}

	for _, panel := range panels {
		panelMap, ok := panel.(map[string]any)
		if !ok {
			continue
		}
		datasource, ok := panelMap["datasource"].(map[string]any)
		if !ok {
			t.Fatalf("panel %q has no datasource", panelMap["title"])
		}
		if datasource["uid"] != "prometheus" {
			t.Fatalf("panel %q expected datasource UID prometheus, got %q", panelMap["title"], datasource["uid"])
		}
	}

	if strings.Contains(string(content), "${DS_PROMETHEUS}") {
		t.Fatal("dashboard contains unresolved ${DS_PROMETHEUS} placeholder")
	}

	assertPanelQueryContains(t, panels, "P95 Webhook Latency", "histogram_quantile(0.95")
	assertPanelQueryContains(t, panels, "P95 Webhook Latency", "payment_webhook_response_duration_seconds_bucket")
	assertPanelQueryContains(t, panels, "P95 Database Write Latency", "histogram_quantile(0.95")
	assertPanelQueryContains(t, panels, "P95 Database Write Latency", "payment_webhook_database_write_duration_seconds_bucket")
	assertPanelQueryContains(t, panels, "Webhook Request Rate", "rate(payment_webhook_requests_total")
	assertPanelQueryContains(t, panels, "Duplicate Event Count", "payment_webhook_duplicates_total")
	assertPanelQueryContains(t, panels, "Anomaly Count", "payment_webhook_anomalies_total")
	assertPanelQueryContains(t, panels, "Provider Traffic Spike", "payment_webhook_provider_events_total")
	assertPanelQueryContains(t, panels, "Payment Event Status Distribution", "payment_webhook_provider_events_total")
	assertPanelQueryContains(t, panels, "DB Pool Connections", "payment_webhook_db_open_connections")
	assertPanelQueryContains(t, panels, "DB Pool Connections", "payment_webhook_db_in_use_connections")
	assertPanelQueryContains(t, panels, "DB Pool Connections", "payment_webhook_db_idle_connections")
	assertPanelQueryContains(t, panels, "DB Pool Wait Rate", "payment_webhook_db_wait_count_total")
	assertPanelQueryContains(t, panels, "DB Pool Wait Duration", "payment_webhook_db_wait_duration_seconds_total")
	assertPanelQueryContains(t, panels, "Webhook Success and Error Rate", "payment_webhook_success_total")
	assertPanelQueryContains(t, panels, "Webhook Success and Error Rate", "payment_webhook_errors_total")
	assertPanelQueryContains(t, panels, "Webhook Success and Error Rate", "payment_webhook_signature_failures_total")
}

func TestDockerComposeMountsGrafanaProvisioningAndDashboardAssets(t *testing.T) {
	t.Parallel()

	content := string(readModuleFile(t, "docker-compose.yml"))

	requiredSnippets := []string{
		"./grafana/provisioning:/etc/grafana/provisioning:ro",
		"./grafana/dashboards:/var/lib/grafana/dashboards:ro",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected docker-compose.yml to contain %q", snippet)
		}
	}
}

func readModuleFile(t *testing.T, parts ...string) []byte {
	t.Helper()

	pathParts := append([]string{"..", ".."}, parts...)
	content, err := os.ReadFile(filepath.Join(pathParts...))
	if err != nil {
		t.Fatalf("read file %v: %v", parts, err)
	}

	return content
}

func dashboardHasPanelTitle(panels []any, title string) bool {
	for _, panel := range panels {
		panelMap, ok := panel.(map[string]any)
		if !ok {
			continue
		}
		if panelMap["title"] == title {
			return true
		}
	}

	return false
}

func assertPanelQueryContains(t *testing.T, panels []any, title, fragment string) {
	t.Helper()

	panel := findPanelByTitle(t, panels, title)
	targets, ok := panel["targets"].([]any)
	if !ok {
		t.Fatalf("panel %q has no targets", title)
	}

	for _, target := range targets {
		targetMap, ok := target.(map[string]any)
		if !ok {
			continue
		}
		expr, _ := targetMap["expr"].(string)
		if strings.Contains(expr, fragment) {
			return
		}
	}

	t.Fatalf("expected panel %q to contain query fragment %q", title, fragment)
}

func findPanelByTitle(t *testing.T, panels []any, title string) map[string]any {
	t.Helper()

	for _, panel := range panels {
		panelMap, ok := panel.(map[string]any)
		if !ok {
			continue
		}
		if panelMap["title"] == title {
			return panelMap
		}
	}

	t.Fatalf("panel %q not found", title)
	return nil
}
