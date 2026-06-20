package tools

import "context"

type readOnlyKeyType struct{}

// InitReadOnlyMode marks ctx as read-only. While set, the mutating file tools
// (Write, Edit, MultiEdit) are denied before their handlers run, so an agent
// invoked with `--mode readonly` cannot modify the working tree regardless of
// what its prompt instructs. This is the hard backstop behind readonly mode:
// prompt wording alone is not enough, since a prompt that omits the
// edit-mode conditional would otherwise let the model apply changes.
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
