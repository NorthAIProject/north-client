package main

import (
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.yaml.in/yaml/v3"
)

const specPath = "../../docs/api/openapi.yaml"

// The iOS client is generated from docs/api/openapi.yaml, so a route the spec
// does not describe is a route the app cannot call, and a path the spec
// describes but the server lacks is a call that 404s in production. Both
// directions are checked against the router the server really mounts.
func TestOpenAPISpecMatchesMountedRoutes(t *testing.T) {
	spec := readSpecOperations(t)
	mounted := mountedAPIOperations(t)

	for _, op := range mounted {
		if !slices.Contains(spec, op) {
			t.Errorf("%s is mounted but not in %s: describe it so the iOS client can call it", op, specPath)
		}
	}
	for _, op := range spec {
		if !slices.Contains(mounted, op) {
			t.Errorf("%s is in %s but not mounted: the generated client would call a route that 404s", op, specPath)
		}
	}
}

func readSpecOperations(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}

	var ops []string
	for path, item := range doc.Paths {
		for method := range item {
			switch method {
			case "get", "put", "post", "delete", "patch":
				ops = append(ops, strings.ToUpper(method)+" "+path)
			}
		}
	}
	return ops
}

func mountedAPIOperations(t *testing.T) []string {
	t.Helper()
	var ops []string
	err := chi.Walk(apiRouter(t), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// chi and OpenAPI both write parameters as {name}; only the version
		// prefix differs, because the spec's server URL carries it.
		ops = append(ops, method+" "+strings.TrimPrefix(route, "/api/v1"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ops
}
