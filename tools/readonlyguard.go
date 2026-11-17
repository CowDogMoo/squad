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

type editsOptionalKeyType struct{}

// InitEditsOptional marks ctx as a run that does not need to produce edits:
// --require-actionable=false, or a remote-only agent. The edit tools still
// work, but nothing pushes the agent toward them: the edit deadline, the
// phase nudges, and read blocking all stay off.
func InitEditsOptional(ctx context.Context) context.Context {
	return context.WithValue(ctx, editsOptionalKeyType{}, true)
}

// EditsOptional reports whether ctx was marked by [InitEditsOptional].
func EditsOptional(ctx context.Context) bool {
	v, ok := ctx.Value(editsOptionalKeyType{}).(bool)
	return ok && v
}

// EditsExpected reports whether the run measures progress in edits, which
// arms the guards that push a reading agent to edit. It is false in readonly
// mode, where the agent cannot edit, and when edits are optional.
func EditsExpected(ctx context.Context) bool {
	return !IsReadOnlyMode(ctx) && !EditsOptional(ctx)
}
