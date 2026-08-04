// Package prompts holds North's prompts as versioned, reviewable files.
//
// Prompts are product surface, not string literals. Keeping them in .md files
// under this package means they can be read, diffed, and reviewed like any
// other code, and it stops a persona from being quietly redefined inside a
// handler somewhere.
//
// No other package may contain a prompt string.
package prompts

import (
	"embed"
	"fmt"
	"strings"
	"sync"
	"text/template"
)

//go:embed *.md
var files embed.FS

var (
	cacheMu sync.RWMutex
	cache   = map[string]*template.Template{}
)

// Names of the available prompts. Constants rather than raw strings so a typo
// is a compile error rather than a runtime one.
const (
	CoachSystem       = "coach_system.md"
	WorkoutPlan       = "workout_plan.md"
	FormAnalysis      = "form_analysis.md"
	ConversationTitle = "conversation_title.md"
	MemoryExtraction  = "memory_extraction.md"
)

// Render executes a prompt template with the given data.
func Render(name string, data any) (string, error) {
	tmpl, err := load(name)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("prompts: execute %s: %w", name, err)
	}

	return strings.TrimSpace(out.String()), nil
}

// MustRender panics on failure. For use at startup or in tests, where a broken
// prompt template means the build is wrong.
func MustRender(name string, data any) string {
	out, err := Render(name, data)
	if err != nil {
		panic(err)
	}
	return out
}

// Raw returns a prompt without template execution.
func Raw(name string) (string, error) {
	content, err := files.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("prompts: read %s: %w", name, err)
	}
	return strings.TrimSpace(string(content)), nil
}

func load(name string) (*template.Template, error) {
	cacheMu.RLock()
	tmpl, ok := cache[name]
	cacheMu.RUnlock()
	if ok {
		return tmpl, nil
	}

	content, err := files.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("prompts: read %s: %w", name, err)
	}

	// missingkey=error: a prompt that silently renders "<no value>" into a
	// coaching instruction is worse than one that fails loudly.
	tmpl, err = template.New(name).Option("missingkey=error").Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("prompts: parse %s: %w", name, err)
	}

	cacheMu.Lock()
	cache[name] = tmpl
	cacheMu.Unlock()

	return tmpl, nil
}

// Names lists every embedded prompt, so a test can assert they all parse.
func Names() ([]string, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
