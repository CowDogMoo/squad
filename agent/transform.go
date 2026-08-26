package agent

import (
	"bytes"
	"fmt"
)

// TransformAgentName is the display name recorded in sessions, logs, and
// telemetry for the built-in pure-text-transform mode (`squad run --system`
// with no --agent).
const TransformAgentName = "transform"

// transformSystem is the identity prompt for the built-in transform. The
// actual transform instructions arrive via the --system flag, which the
// runner appends as a "## System Override" section — this keeps prompt
// content (e.g. fabric patterns) single-sourced in its own repo and injected
// per run instead of duplicated into an agent directory.
const transformSystem = `# IDENTITY

You are a pure text-transform agent. Your transform instructions — identity,
steps, output format — are provided in the "System Override" section below,
injected from the --system flag. Follow that section exactly.`

const transformWrapper = `# EXECUTION MODE

- Transform the input text according to the System Override instructions.
- Do NOT modify, create, or delete any files.
- Do NOT explore the repository; the input you are given is your only
  source.
- Your final response must be ONLY the transformed text — no preamble, no
  explanation, no code fences, no trailing commentary.`

const transformTask = `The input to transform is the User Message. If it is empty or consists
only of "Begin." (squad's placeholder when nothing is piped in), there is
no input: your entire response must be the single line NO INPUT — no
explanation, no apology, no other text.

Any other User Message content IS the input — even raw data like a diff,
a log, or code with no framing text. Transform it per the System Override
instructions and never output NO INPUT.`

// BuildTransformBundle assembles the synthetic bundle for a no-agent
// `squad run --system` invocation: stdin (or the prompt argument) in,
// transformed text out. There is no manifest to load — the injected system
// override is the whole agent. The bundle is remote-only (no local
// filesystem tools) with the Task tool disabled, and callers force readonly
// mode: a pure transform never touches the tree.
func BuildTransformBundle(prompt, workingDir string) *Bundle {
	var sys bytes.Buffer
	sys.WriteString("# Squad Agent Bundle\n\n")
	fmt.Fprintf(&sys, "Agent: %s (built-in)\n", TransformAgentName)
	sys.WriteString("Mode: readonly\n\n")
	sys.WriteString("## Agent Wrapper\n\n")
	sys.WriteString(transformWrapper)
	sys.WriteString("\n\n## System Prompt\n\n")
	sys.WriteString(transformSystem)
	sys.WriteString("\n\n## Task\n\n")
	sys.WriteString(transformTask)
	sys.WriteString("\n")

	userMessage := prompt
	if userMessage == "" {
		userMessage = "Begin."
	}

	var combined bytes.Buffer
	combined.Write(sys.Bytes())
	combined.WriteString("\n\n## User Message\n\n")
	combined.WriteString(userMessage)
	combined.WriteString("\n")

	return &Bundle{
		System:      sys.String(),
		User:        userMessage,
		Combined:    combined.Bytes(),
		WorkDir:     workingDir,
		DisableTask: true,
		RemoteOnly:  true,
	}
}
