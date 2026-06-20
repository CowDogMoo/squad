package tools

import "context"

type readOnlyKeyType struct{}

// InitReadOnlyMode marks ctx as read-only, denying the mutating file tools
// (Write, Edit, MultiEdit) before their handlers run. This is the hard backstop
// behind `--mode readonly`: it holds even when an agent's prompt omits the
// edit-mode conditional and asks the model to apply changes.
func InitReadOnlyMode(ctx context.Context) context.Context {
	return context.WithValue(ctx, readOnlyKeyType{}, true)
}

// IsReadOnlyMode reports whether ctx is in read-only mode.
func IsReadOnlyMode(ctx context.Context) bool {
	v, ok := ctx.Value(readOnlyKeyType{}).(bool)
	return ok && v
}

// IsMutatingTool reports whether a tool name writes to the working tree.
func IsMutatingTool(name string) bool {
	return name == "Edit" || name == "MultiEdit" || name == "Write"
}
