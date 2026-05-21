// Skill tool — Level 2 progressive disclosure for Anthropic Agent Skills.
//
// When the agent calls Skill(name), this handler:
//   1. Looks up `name` in the run's catalog (assembled at agent boot).
//   2. Pushes the skill's directory onto the run's skill stack.
//   3. Returns the full SKILL.md body so the agent can follow it.
//
// Once a skill is on the stack, the Read/Bash anchor relaxation in this
// package permits the agent to read references and run scripts inside that
// directory, in addition to the working directory.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/skill"
	"github.com/tmc/langchaingo/llms"
)

// SkillRuntime bundles the per-run skill state the Skill tool needs. Entries
// is the filtered set the agent's system prompt already lists — the catalog
// is filtered upstream (in agent.BuildBundleWithOptions) so the Skill tool
// can only load what the model was told about. Stack is mutated as skills
// are loaded.
type SkillRuntime struct {
	Entries []skill.Entry
	Stack   *skill.Stack
	// OnLoad, when non-nil, is invoked each time Skill(name) successfully
	// pushes an entry. Used by the runner to emit a session event.
	OnLoad func(entry skill.Entry)
}

// HasCatalog reports whether r is non-nil and exposes at least one skill.
// The Skill tool is only registered when this is true.
func (r *SkillRuntime) HasCatalog() bool {
	return r != nil && len(r.Entries) > 0
}

// find returns the runtime entry matching name, or ok=false. Walks Entries
// linearly — the catalog is small (typically <50 skills) so the simple
// loop beats a map allocation per run.
func (r *SkillRuntime) find(name string) (skill.Entry, bool) {
	if r == nil {
		return skill.Entry{}, false
	}
	for _, e := range r.Entries {
		if e.Name() == name {
			return e, true
		}
	}
	return skill.Entry{}, false
}

type skillRuntimeKey struct{}

// WithSkillRuntime returns ctx with the runtime attached. Subsequent tool
// calls can recover it via GetSkillRuntime to consult the stack or catalog.
func WithSkillRuntime(ctx context.Context, r *SkillRuntime) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, skillRuntimeKey{}, r)
}

// GetSkillRuntime returns the runtime stored on ctx, or nil if none was set.
func GetSkillRuntime(ctx context.Context) *SkillRuntime {
	r, _ := ctx.Value(skillRuntimeKey{}).(*SkillRuntime)
	return r
}

// GetSkillStack is a convenience for callers that only need the stack — most
// of the file tools just want path-containment lookups.
func GetSkillStack(ctx context.Context) *skill.Stack {
	r := GetSkillRuntime(ctx)
	if r == nil {
		return nil
	}
	return r.Stack
}

// withSkillEnv prepends `export SQUAD_SKILL_DIR=...;` to command when a skill
// is on the stack, so bundled scripts can resolve sibling files regardless
// of cwd. Returns command unchanged when no skill is active.
//
// This works across all executor backends because every executor passes the
// command string to a shell wrapper (bash -lc, sh -c, etc.) — the prepended
// export runs in the same shell session as the agent's command.
func withSkillEnv(ctx context.Context, command string) string {
	stack := GetSkillStack(ctx)
	if stack == nil {
		return command
	}
	top, ok := stack.Top()
	if !ok || top.Dir == "" {
		return command
	}
	return "export SQUAD_SKILL_DIR=" + shellSingleQuote(top.Dir) + "; " + command
}

// shellSingleQuote wraps s in POSIX single quotes, escaping any embedded
// quote with the standard `'\”` sequence so the result is safe to splice
// into a shell command.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// definitionSkill is the LLM-facing tool schema.
func definitionSkill() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "Skill",
			Description: "Load an Agent Skill's full instructions into context. Pass the skill's exact name as listed in the Available skills section of your system prompt. After this call returns you may Read and Bash files inside the skill's directory (references, scripts, assets).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Exact skill name from the Available skills list.",
					},
				},
				"required": []string{"name"},
			},
		},
	}
}

type skillArgs struct {
	Name string `json:"name"`
}

// skillTool returns the Handler.Call function. The runtime is closed over so
// the handler always sees the run's catalog/stack, even though tool
// invocations happen via the generic Handler interface.
func skillTool(runtime *SkillRuntime) func(ctx context.Context, rawArgs []byte) (string, error) {
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var args skillArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse Skill args: %w", err)
		}
		if args.Name == "" {
			return "", fmt.Errorf("skill: name is required")
		}
		if !runtime.HasCatalog() {
			return "", fmt.Errorf("skill: no skill catalog is registered for this run")
		}
		entry, ok := runtime.find(args.Name)
		if !ok {
			return "", fmt.Errorf("skill: %q is not in the catalog", args.Name)
		}
		if runtime.Stack != nil {
			runtime.Stack.Push(entry)
		}
		if runtime.OnLoad != nil {
			runtime.OnLoad(entry)
		}
		logging.DebugContext(ctx, "Skill loaded: name=%s scope=%s dir=%s",
			entry.Name(), entry.Scope.String(), entry.Dir)
		return entry.Manifest.Body, nil
	}
}
