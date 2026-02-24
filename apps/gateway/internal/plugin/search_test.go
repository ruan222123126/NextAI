package plugin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func clearSearchEnvVars(t *testing.T) {
	t.Setenv(searchSerpAPIKeyEnv, "")
	t.Setenv(searchSerpAPIBaseEnv, "")
	t.Setenv(searchTavilyKeyEnv, "")
	t.Setenv(searchTavilyBaseEnv, "")
	t.Setenv(searchBraveKeyEnv, "")
	t.Setenv(searchBraveBaseEnv, "")
	t.Setenv(searchDefaultProviderEnv, "")
}

func TestNewSearchToolFromEnvRequiresProvider(t *testing.T) {
	clearSearchEnvVars(t)
	_, err := NewSearchToolFromEnv()
	if !errors.Is(err, ErrSearchToolProvidersMissing) {
		t.Fatalf("expected ErrSearchToolProvidersMissing, got=%v", err)
	}
}

func TestSearchToolInvokeSerpAPI(t *testing.T) {
	clearSearchEnvVars(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "nextai" {
			t.Fatalf("unexpected query: %q", got)
		}
		if got := r.URL.Query().Get("api_key"); got != "serp-key" {
			t.Fatalf("unexpected api_key: %q", got)
		}
		_, _ = w.Write([]byte(`{"organic_results":[{"title":"NextAI","link":"https://example.com","snippet":"desc"}]}`))
	}))
	defer server.Close()

	t.Setenv(searchSerpAPIKeyEnv, "serp-key")
	t.Setenv(searchSerpAPIBaseEnv, server.URL+"/search.json")
	tool, err := NewSearchToolFromEnv()
	if err != nil {
		t.Fatalf("new search tool failed: %v", err)
	}

	out, invokeErr := tool.Invoke(ToolCommand{
		Items: []ToolCommandItem{
			{Query: "nextai", Count: 3},
		},
	})
	if invokeErr != nil {
		t.Fatalf("invoke failed: %v", invokeErr)
	}
	result, err := out.ToMap()
	if err != nil {
		t.Fatalf("convert result failed: %v", err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got=%#v", result["ok"])
	}
	results, _ := result["results"].([]interface{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got=%d", len(results))
	}
	first, _ := results[0].(map[string]interface{})
	if got, _ := first["url"].(string); got != "https://example.com" {
		t.Fatalf("unexpected url: %q", got)
	}
}

func TestSearchToolInvokeTavilyWithProviderOverride(t *testing.T) {
	clearSearchEnvVars(t)
	serp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"organic_results":[{"title":"Serp","link":"https://serp.example"}]}`))
	}))
	defer serp.Close()

	tavily := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if body["query"] != "multi provider" {
			t.Fatalf("unexpected query in tavily body: %#v", body["query"])
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"Tavily","url":"https://tavily.example","content":"snippet"}]}`))
	}))
	defer tavily.Close()

	t.Setenv(searchSerpAPIKeyEnv, "serp-key")
	t.Setenv(searchSerpAPIBaseEnv, serp.URL)
	t.Setenv(searchTavilyKeyEnv, "tavily-key")
	t.Setenv(searchTavilyBaseEnv, tavily.URL)
	t.Setenv(searchDefaultProviderEnv, searchProviderSerpAPI)

	tool, err := NewSearchToolFromEnv()
	if err != nil {
		t.Fatalf("new search tool failed: %v", err)
	}

	out, invokeErr := tool.Invoke(ToolCommand{
		Items: []ToolCommandItem{
			{
				Query:    "multi provider",
				Provider: "tavily",
			},
		},
	})
	if invokeErr != nil {
		t.Fatalf("invoke failed: %v", invokeErr)
	}
	result, err := out.ToMap()
	if err != nil {
		t.Fatalf("convert result failed: %v", err)
	}
	if got, _ := result["provider"].(string); got != "tavily" {
		t.Fatalf("expected provider=tavily, got=%q", got)
	}
	results, _ := result["results"].([]interface{})
	first, _ := results[0].(map[string]interface{})
	if len(results) != 1 || first["title"] != "Tavily" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestSearchToolRejectsInvalidInput(t *testing.T) {
	clearSearchEnvVars(t)
	t.Setenv(searchSerpAPIKeyEnv, "serp-key")
	tool, err := NewSearchToolFromEnv()
	if err != nil {
		t.Fatalf("new search tool failed: %v", err)
	}

	_, invokeErr := tool.Invoke(ToolCommand{
		Items: []ToolCommandItem{
			{},
		},
	})
	if !errors.Is(invokeErr, ErrSearchToolQueryMissing) {
		t.Fatalf("expected ErrSearchToolQueryMissing, got=%v", invokeErr)
	}

	_, invokeErr = tool.Invoke(ToolCommand{
		Items: []ToolCommandItem{
			{Query: "nextai", Provider: "duckduckgo"},
		},
	})
	if !errors.Is(invokeErr, ErrSearchToolProviderUnsupported) {
		t.Fatalf("expected ErrSearchToolProviderUnsupported, got=%v", invokeErr)
	}
}
