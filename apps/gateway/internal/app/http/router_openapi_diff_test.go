package transport

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

var contractHTTPMethods = map[string]struct{}{
	http.MethodGet:    {},
	http.MethodPost:   {},
	http.MethodPut:    {},
	http.MethodPatch:  {},
	http.MethodDelete: {},
}

func TestRuntimeRoutesMatchOpenAPI(t *testing.T) {
	t.Parallel()

	runtimeOps := collectRuntimeOperations(t)
	openAPIOps := collectOpenAPIOperations(t)

	missingInOpenAPI := diffOperations(runtimeOps, openAPIOps)
	missingInRuntime := diffOperations(openAPIOps, runtimeOps)
	if len(missingInOpenAPI) == 0 && len(missingInRuntime) == 0 {
		return
	}

	lines := []string{"runtime routes and OpenAPI routes are out of sync"}
	if len(missingInOpenAPI) > 0 {
		lines = append(lines, "missing in OpenAPI:")
		for _, op := range missingInOpenAPI {
			lines = append(lines, "- "+op)
		}
	}
	if len(missingInRuntime) > 0 {
		lines = append(lines, "missing in runtime router:")
		for _, op := range missingInRuntime {
			lines = append(lines, "- "+op)
		}
	}
	t.Fatal(strings.Join(lines, "\n"))
}

func collectRuntimeOperations(t *testing.T) map[string]map[string]struct{} {
	t.Helper()

	router := NewRouter("test-api-key", newNoOpHandlers(), nil)
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("router does not implement chi.Routes: %T", router)
	}

	ops := map[string]map[string]struct{}{}
	if err := chi.Walk(routes, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		method = strings.ToUpper(strings.TrimSpace(method))
		if !isContractHTTPMethod(method) {
			return nil
		}
		addOperation(ops, normalizePathPattern(route), method)
		return nil
	}); err != nil {
		t.Fatalf("walk runtime routes failed: %v", err)
	}

	return ops
}

func collectOpenAPIOperations(t *testing.T) map[string]map[string]struct{} {
	t.Helper()

	specPath := openAPISpecPath(t)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi file failed (%s): %v", specPath, err)
	}

	ops := map[string]map[string]struct{}{}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	inPaths := false
	currentPath := ""
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := leadingIndent(line)
		if !inPaths {
			if trimmed == "paths:" {
				inPaths = true
			}
			continue
		}

		if indent == 0 && strings.HasSuffix(trimmed, ":") {
			break
		}
		if indent == 2 && strings.HasSuffix(trimmed, ":") && strings.HasPrefix(trimmed, "/") {
			currentPath = strings.Trim(strings.TrimSuffix(trimmed, ":"), `"'`)
			continue
		}
		if currentPath == "" {
			continue
		}
		if indent == 4 && strings.HasSuffix(trimmed, ":") {
			method := strings.ToUpper(strings.Trim(strings.TrimSuffix(trimmed, ":"), `"'`))
			if isContractHTTPMethod(method) {
				addOperation(ops, normalizePathPattern(currentPath), method)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan openapi file failed: %v", err)
	}
	if !inPaths {
		t.Fatalf("openapi file missing paths section: %s", specPath)
	}

	return ops
}

func openAPISpecPath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file path failed")
	}
	specPath := filepath.Clean(filepath.Join(
		filepath.Dir(filename),
		"..", "..", "..", "..", "..",
		"packages", "contracts", "openapi", "openapi.yaml",
	))
	if _, err := os.Stat(specPath); err != nil {
		t.Fatalf("openapi spec not found: %s (%v)", specPath, err)
	}
	return specPath
}

func newNoOpHandlers() Handlers {
	handlers := Handlers{}
	fillNoOpHandlerFuncs(reflect.ValueOf(&handlers).Elem())
	return handlers
}

func fillNoOpHandlerFuncs(value reflect.Value) {
	if !value.IsValid() {
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			fillNoOpHandlerFuncs(value.Field(i))
		}
	case reflect.Func:
		if value.CanSet() && value.Type() == reflect.TypeOf(http.HandlerFunc(nil)) {
			value.Set(reflect.ValueOf(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
		}
	}
}

func isContractHTTPMethod(method string) bool {
	_, ok := contractHTTPMethods[method]
	return ok
}

func addOperation(ops map[string]map[string]struct{}, path string, method string) {
	methods, ok := ops[path]
	if !ok {
		methods = map[string]struct{}{}
		ops[path] = methods
	}
	methods[method] = struct{}{}
}

func diffOperations(left map[string]map[string]struct{}, right map[string]map[string]struct{}) []string {
	diff := make([]string, 0)
	for path, methods := range left {
		for method := range methods {
			rightMethods, ok := right[path]
			if !ok {
				diff = append(diff, method+" "+path)
				continue
			}
			if _, ok := rightMethods[method]; !ok {
				diff = append(diff, method+" "+path)
			}
		}
	}
	sort.Strings(diff)
	return diff
}

func normalizePathPattern(path string) string {
	path = strings.Trim(path, `"'`)
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	for len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}

	segments := strings.Split(path, "/")
	for i := 0; i < len(segments); i++ {
		segment := strings.TrimSpace(segments[i])
		switch {
		case segment == "*":
			segments[i] = "{}"
		case strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}"):
			segments[i] = "{}"
		default:
			segments[i] = segment
		}
	}
	path = strings.Join(segments, "/")
	if path == "" {
		return "/"
	}
	return path
}

func leadingIndent(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
			continue
		}
		if ch == '\t' {
			count += 2
			continue
		}
		break
	}
	return count
}
