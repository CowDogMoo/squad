// Package scaffold creates new agents and pipelines from embedded templates.
package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"path"
	"strings"
	"text/template"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

//go:embed templates/*.tmpl
var AgentTemplates embed.FS

const templatesDir = "templates"

// AgentData contains the data for rendering agent templates.
type AgentData struct {
	Name        string // e.g. "xss-testing"
	NameTitle   string // e.g. "XSS Testing"
	Description string // e.g. "XSS vulnerability scanner"
	Lang        string // e.g. "go", "python", "bash", "ansible", "generic"
	Version     string // e.g. "0.1.0"
}

// Render executes a template by name with the given data.
func Render(name string, data AgentData) (string, error) {
	content, err := AgentTemplates.ReadFile(path.Join(templatesDir, name))
	if err != nil {
		return "", fmt.Errorf("failed to read template %s: %w", name, err)
	}

	titleCaser := cases.Title(language.English)
	funcMap := template.FuncMap{
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"title": titleCaser.String,
	}

	tmpl, err := template.New(name).Funcs(funcMap).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", name, err)
	}

	return buf.String(), nil
}

// ListTemplates returns the names of all embedded template files.
func ListTemplates() ([]string, error) {
	entries, err := AgentTemplates.ReadDir(templatesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read templates directory: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tmpl") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
