package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/cowdogmoo/squad/csync"
	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

type readCacheKeyType struct{}

// ReadCacheEntry records metadata about a file that has already been read
// during the current tool-loop session.
type ReadCacheEntry struct {
	ContentHash     string // SHA-256 of file content
	Lines           int    // number of lines
	Bytes           int    // file size
	Iteration       int    // iteration when first read
	Summary         string // brief content summary (top-level declarations) for compaction
	CompactionEpoch int    // compaction epoch when this entry was last served
}

// cacheHitStreakThreshold is the number of consecutive cache-hit Read calls
// (without any edit or new-file read) before the circuit breaker fires.
const cacheHitStreakThreshold = 3

// ReadCache tracks files already read in the current session to avoid
// wasting tokens on redundant re-reads.
type ReadCache struct {
	entries         *csync.Map[string, ReadCacheEntry]
	mu              sync.Mutex
	cacheStreak     int // consecutive cache-hit Read calls without edit or new read
	compactionEpoch int // incremented each time context compaction occurs
}

// NewReadCache creates an empty ReadCache.
func NewReadCache() *ReadCache {
	return &ReadCache{entries: csync.NewMap[string, ReadCacheEntry]()}
}

// InitReadCache attaches a new ReadCache to ctx for the duration of a run.
func InitReadCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, readCacheKeyType{}, NewReadCache())
}

// GetReadCache retrieves the ReadCache from ctx, or nil if not set.
func GetReadCache(ctx context.Context) *ReadCache {
	if rc, ok := ctx.Value(readCacheKeyType{}).(*ReadCache); ok {
		return rc
	}
	return nil
}

// HashContent returns a hex-encoded SHA-256 hash of data.
func HashContent(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// BumpCompactionEpoch increments the compaction epoch. Call this after
// context compaction so that subsequent cache hits know the content has
// been evicted from the conversation and should be served again.
func (rc *ReadCache) BumpCompactionEpoch() {
	if rc == nil {
		return
	}
	rc.mu.Lock()
	rc.compactionEpoch++
	rc.mu.Unlock()
}

// CompactionEpoch returns the current compaction epoch for the cache.
func (rc *ReadCache) CompactionEpoch() int {
	if rc == nil {
		return 0
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.compactionEpoch
}

// Has reports whether a path exists in the cache (regardless of hash).
// Used to distinguish "new file read" from "re-read" when deciding
// whether to block reads.
func (rc *ReadCache) Has(path string) bool {
	if rc == nil {
		return false
	}
	_, exists := rc.entries.Get(path)
	return exists
}

// Check returns the cached entry if the file content is unchanged.
// Returns (entry, true) if cached with same hash, (zero, false) otherwise.
func (rc *ReadCache) Check(path string, contentHash string) (ReadCacheEntry, bool) {
	if rc == nil {
		return ReadCacheEntry{}, false
	}
	entry, exists := rc.entries.Get(path)
	if exists && entry.ContentHash == contentHash {
		return entry, true
	}
	return ReadCacheEntry{}, false
}

// IsStaleAfterCompaction reports whether the entry was stored before the
// most recent compaction, meaning its content has been evicted from the
// conversation and should be served again on the next read.
func (rc *ReadCache) IsStaleAfterCompaction(entry ReadCacheEntry) bool {
	if rc == nil {
		return false
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return entry.CompactionEpoch < rc.compactionEpoch
}

// Store records a file read in the cache and resets the cache-hit streak
// (reading a new/changed file is progress, not a loop).
func (rc *ReadCache) Store(path, contentHash string, lines, bytes, iteration int, summary string) {
	if rc == nil {
		return
	}
	rc.mu.Lock()
	epoch := rc.compactionEpoch
	rc.cacheStreak = 0
	rc.mu.Unlock()
	rc.entries.Set(path, ReadCacheEntry{
		ContentHash:     contentHash,
		Lines:           lines,
		Bytes:           bytes,
		Iteration:       iteration,
		Summary:         summary,
		CompactionEpoch: epoch,
	})
}

// IncrementStreak increments and returns the consecutive cache-hit streak counter.
func (rc *ReadCache) IncrementStreak() int {
	if rc == nil {
		return 0
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.cacheStreak++
	return rc.cacheStreak
}

// ResetStreak resets the consecutive cache-hit streak counter to zero.
func (rc *ReadCache) ResetStreak() {
	if rc == nil {
		return
	}
	rc.mu.Lock()
	rc.cacheStreak = 0
	rc.mu.Unlock()
}

// Summaries returns a map of path → human-readable summary for all cached files.
// Used by CompactionSummary to preserve file context after compaction.
func (rc *ReadCache) Summaries() map[string]string {
	if rc == nil {
		return nil
	}
	result := make(map[string]string)
	rc.entries.Range(func(path string, entry ReadCacheEntry) bool {
		if entry.Summary != "" {
			result[path] = fmt.Sprintf("%d lines; %s", entry.Lines, entry.Summary)
		} else {
			result[path] = fmt.Sprintf("%d lines", entry.Lines)
		}
		return true
	})
	return result
}

// Len returns the number of files currently tracked in the read cache.
func (rc *ReadCache) Len() int {
	if rc == nil {
		return 0
	}
	return rc.entries.Len()
}

type iterationKeyType struct{}

// InitIterationCounter attaches an iteration counter (starting at 0) to ctx.
func InitIterationCounter(ctx context.Context) context.Context {
	counter := 0
	return context.WithValue(ctx, iterationKeyType{}, &counter)
}

// SetIteration updates the current iteration index stored on ctx.
func SetIteration(ctx context.Context, i int) {
	if p, ok := ctx.Value(iterationKeyType{}).(*int); ok {
		*p = i
	}
}

// GetIteration returns the current iteration index from ctx, or 0 if not set.
func GetIteration(ctx context.Context) int {
	if p, ok := ctx.Value(iterationKeyType{}).(*int); ok {
		return *p
	}
	return 0
}

type phaseEnforcerKeyType struct{}

// SetPhaseEnforcer stores pe on ctx so tool handlers can retrieve it.
func SetPhaseEnforcer(ctx context.Context, pe *PhaseEnforcer) context.Context {
	return context.WithValue(ctx, phaseEnforcerKeyType{}, pe)
}

// GetPhaseEnforcer retrieves the PhaseEnforcer from ctx, or nil if not set.
func GetPhaseEnforcer(ctx context.Context) *PhaseEnforcer {
	if pe, ok := ctx.Value(phaseEnforcerKeyType{}).(*PhaseEnforcer); ok {
		return pe
	}
	return nil
}

// PhaseEnforcer tracks whether an agent is making progress (editing files)
// and injects escalating nudge messages when the agent spends too long exploring.
// After multiple ignored nudges, ShouldBlockReads returns true to force the
// model to stop reading and start editing.
type PhaseEnforcer struct {
	// NudgeAfter is the number of consecutive read-only iterations before
	// injecting the first nudge message telling the agent to start editing.
	NudgeAfter int

	readOnlyIters int
	nudgeCount    int // how many nudges have been sent
	editSeen      bool
}

// NudgesSent returns the number of nudge messages sent so far.
func (pe *PhaseEnforcer) NudgesSent() int {
	if pe == nil {
		return 0
	}
	return pe.nudgeCount
}

// ResetReadProgress resets the read-only iteration counter and nudge
// count. Call this when the agent made discovery progress (read new
// files) so the enforcer stops treating it as stuck in a loop.
func (pe *PhaseEnforcer) ResetReadProgress() {
	if pe == nil || pe.editSeen {
		return
	}
	pe.readOnlyIters = 0
	pe.nudgeCount = 0
}

// ShouldBlockReads returns true when 2+ nudges have been sent and ignored,
// indicating the model is stuck in a read loop and reads should be blocked.
func (pe *PhaseEnforcer) ShouldBlockReads() bool {
	if pe == nil || pe.editSeen {
		return false
	}
	return pe.nudgeCount >= 2
}

// NewPhaseEnforcer creates a PhaseEnforcer that nudges after n read-only
// iterations. Returns nil if n <= 0 (disabled).
func NewPhaseEnforcer(nudgeAfter int) *PhaseEnforcer {
	if nudgeAfter <= 0 {
		return nil
	}
	return &PhaseEnforcer{NudgeAfter: nudgeAfter}
}

// ObserveTools inspects the tool calls for the current iteration and returns
// a nudge message if the agent should be prompted to start editing.
// Nudges escalate: first is a gentle check, subsequent ones are increasingly
// forceful. Returns empty string if no nudge is needed.
// Note: this does NOT set editSeen — call ConfirmEdit after verifying success.
func (pe *PhaseEnforcer) ObserveTools(toolNames []string) string {
	if pe == nil || pe.editSeen {
		return ""
	}
	for _, name := range toolNames {
		if isEditTool(name) {
			return ""
		}
	}
	pe.readOnlyIters++

	// First nudge fires at NudgeAfter. Subsequent nudges fire every
	// NudgeAfter iterations thereafter, with escalating urgency.
	if pe.readOnlyIters >= pe.NudgeAfter && (pe.readOnlyIters-pe.NudgeAfter)%pe.NudgeAfter == 0 {
		pe.nudgeCount++
		return pe.nudgeMessage()
	}
	return ""
}

// ConfirmEdit marks the phase enforcer as having seen a successful edit.
func (pe *PhaseEnforcer) ConfirmEdit(toolCalls []llms.ToolCall, results map[string]string) {
	if pe == nil || pe.editSeen {
		return
	}
	for _, tc := range toolCalls {
		if tc.FunctionCall == nil || !isEditTool(tc.FunctionCall.Name) {
			continue
		}
		result, ok := results[tc.ID]
		if !ok {
			continue
		}
		if strings.HasPrefix(result, "error:") ||
			strings.Contains(result, "text not found") {
			continue
		}
		pe.editSeen = true
		return
	}
}

// nudgeMessage returns an escalating nudge based on how many have been sent.
func (pe *PhaseEnforcer) nudgeMessage() string {
	switch pe.nudgeCount {
	case 1:
		return fmt.Sprintf(
			"[PROGRESS CHECK] You have spent %d iterations reading/analyzing without making any edits. "+
				"You should have enough context by now. Start making changes NOW. "+
				"Do not read more files unless absolutely necessary for your next edit. "+
				"Batch multiple Edit calls in a single response.",
			pe.readOnlyIters)
	case 2:
		return fmt.Sprintf(
			"[URGENT — STOP READING] You have spent %d iterations without a single Edit call. "+
				"Your Read calls are returning cached results — you already have the file contents. "+
				"Call Edit or Write in your NEXT response. If you call Read again instead of Edit, "+
				"you are wasting your iteration budget.",
			pe.readOnlyIters)
	default:
		return fmt.Sprintf(
			"[FINAL WARNING] %d iterations with ZERO edits. You are in a read loop. "+
				"STOP calling Read/Glob/Grep. Call Edit NOW with the file contents you already have. "+
				"Your next tool call MUST be Edit or Write — anything else is budget waste.",
			pe.readOnlyIters)
	}
}

type compactionStats struct {
	filesRead        map[string]bool
	patternsSearched map[string]bool
	editsApplied     map[string]int
	commandsRun      []string
}

func collectCompactionStats(messages []llms.MessageContent) compactionStats {
	s := compactionStats{
		filesRead:        make(map[string]bool),
		patternsSearched: make(map[string]bool),
		editsApplied:     make(map[string]int),
	}
	for _, msg := range messages {
		if msg.Role != llms.ChatMessageTypeAI {
			continue
		}
		for _, part := range msg.Parts {
			tc, ok := part.(llms.ToolCall)
			if !ok || tc.FunctionCall == nil {
				continue
			}
			classifyToolCall(&s, tc.FunctionCall.Name, tc.FunctionCall.Arguments)
		}
	}
	return s
}

func classifyToolCall(s *compactionStats, name, args string) {
	switch name {
	case "Read":
		if path := extractJSONField(args, "path"); path != "" {
			s.filesRead[path] = true
		}
	case "Grep":
		if pat := extractJSONField(args, "pattern"); pat != "" {
			s.patternsSearched[pat] = true
		}
	case "Edit", "MultiEdit", "Write":
		if path := extractJSONField(args, "path"); path != "" {
			s.editsApplied[path]++
		}
	case "Bash":
		if cmd := extractJSONField(args, "command"); cmd != "" {
			s.commandsRun = append(s.commandsRun, TruncateString(cmd, 80))
		}
	}
}

// writeCappedList writes a label, a capped list of items, and a newline.
func writeCappedList(sb *strings.Builder, label string, items []string, cap int) {
	sb.WriteString(label)
	if len(items) > cap {
		fmt.Fprintf(sb, "%s ... and %d more", strings.Join(items[:cap], ", "), len(items)-cap)
	} else {
		sb.WriteString(strings.Join(items, ", "))
	}
	sb.WriteString("\n")
}

// CompactionSummary extracts structured information from messages before
// they are compacted, preserving the agent's "mental map" of what it has done.
// When rc is non-nil, per-file summaries (line count + declarations) are
// included so the model can write edits without re-reading files.
func CompactionSummary(messages []llms.MessageContent, rc *ReadCache) string {
	s := collectCompactionStats(messages)

	if len(s.filesRead) == 0 && len(s.editsApplied) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[SESSION STATE after context compaction]\n")

	if len(s.filesRead) > 0 {
		summaries := rc.Summaries()
		if len(summaries) > 0 {
			sb.WriteString("Files previously read (do NOT re-read — use these summaries to write edits):\n")
			keys := sortedKeys(s.filesRead)
			cap := 30
			for i, path := range keys {
				if i >= cap {
					fmt.Fprintf(&sb, "  ... and %d more files\n", len(keys)-cap)
					break
				}
				if summary, ok := summaries[path]; ok {
					fmt.Fprintf(&sb, "  %s — %s\n", path, summary)
				} else {
					fmt.Fprintf(&sb, "  %s\n", path)
				}
			}
		} else {
			writeCappedList(&sb, "Files read: ", sortedKeys(s.filesRead), 30)
		}
	}
	if len(s.patternsSearched) > 0 {
		writeCappedList(&sb, "Patterns searched: ", sortedKeys(s.patternsSearched), 15)
	}
	if len(s.editsApplied) > 0 {
		var entries []string
		for path, count := range s.editsApplied {
			entries = append(entries, fmt.Sprintf("%s (%d)", path, count))
		}
		writeCappedList(&sb, "Edits applied: ", entries, 30)
	}
	if len(s.commandsRun) > 0 {
		if len(s.commandsRun) > 10 {
			s.commandsRun = s.commandsRun[:10]
		}
		sb.WriteString("Commands run: ")
		sb.WriteString(strings.Join(s.commandsRun, "; "))
		sb.WriteString("\n")
	}

	sb.WriteString("Do NOT re-read files listed above — write edits from the summaries and your notes.\n")
	return sb.String()
}

// extractJSONField does a quick, non-strict extraction of a string field from JSON.
// This avoids the overhead of full JSON parsing for simple cases.
func extractJSONField(jsonStr, field string) string {
	// Look for "field":"value" or "field": "value"
	key := fmt.Sprintf(`"%s"`, field)
	idx := strings.Index(jsonStr, key)
	if idx < 0 {
		return ""
	}
	rest := jsonStr[idx+len(key):]
	// Skip : and whitespace
	rest = strings.TrimLeft(rest, ": \t\n")
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// sortedKeys returns map keys sorted alphabetically.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort — these slices are small
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

const (
	// largeFileLineThreshold is the number of lines above which Read
	// auto-truncates to head+tail and suggests using offset/limit.
	largeFileLineThreshold = 500
	// warnFileLineThreshold is the number of lines above which Read
	// prepends a warning about file size.
	warnFileLineThreshold = 200
)

// SmartReadResult describes how to handle a file read based on its size.
type SmartReadResult struct {
	Action  string // "full", "warn", "truncate"
	Warning string // prepended warning (for "warn" and "truncate")
}

// ClassifyFileSize determines the read strategy for a file.
func ClassifyFileSize(lineCount, byteCount int) SmartReadResult {
	if lineCount > largeFileLineThreshold {
		return SmartReadResult{
			Action: "truncate",
			Warning: fmt.Sprintf(
				"[Large file: %d lines, %d bytes. Showing first %d + last %d lines. Use Read with offset/limit for specific sections.]",
				lineCount, byteCount, largeFileLineThreshold*3/4, largeFileLineThreshold/4),
		}
	}
	if lineCount > warnFileLineThreshold {
		return SmartReadResult{
			Action: "warn",
			Warning: fmt.Sprintf(
				"[File has %d lines. Consider using Read with offset/limit for targeted reads in the future.]",
				lineCount),
		}
	}
	return SmartReadResult{Action: "full"}
}

// TruncateToLines returns the first headLines and last tailLines of the content.
func TruncateToLines(content string, headLines, tailLines int) string {
	lines := strings.SplitAfter(content, "\n")
	total := len(lines)
	if total > 0 && lines[total-1] == "" {
		lines = lines[:total-1]
		total = len(lines)
	}
	if total <= headLines+tailLines {
		return content
	}

	head := strings.Join(lines[:headLines], "")
	tail := strings.Join(lines[total-tailLines:], "")
	omitted := total - headLines - tailLines

	return fmt.Sprintf("%s\n... [%d lines omitted — use Read with offset/limit] ...\n\n%s", head, omitted, tail)
}

// ToolEfficiencyPrompt is injected into all agent system prompts to encourage
// batching of tool calls and efficient file reading.
const ToolEfficiencyPrompt = `## Tool Efficiency

When you need to perform multiple independent operations, invoke ALL relevant tools in a single response:
- Reading multiple files: call Read for ALL of them in one response, not one at a time.
- Making multiple edits to ONE file: use MultiEdit to batch all changes in a single call.
- Making edits across DIFFERENT files: call Edit or MultiEdit for ALL of them in one response.
- Multiple searches: call Grep for ALL patterns in one response.

Do NOT read files you have already read unless you need to verify edits you just made.
Do NOT spend more than a few iterations exploring before making your first edit.
Prefer using Read with offset/limit for large files instead of reading the entire file.
`

// ProgressCheckPrompt is injected at 25% budget to check if the agent is making progress.
const ProgressCheckPrompt = "[PROGRESS CHECK] You have used 25%% of your cost budget ($%.2f remaining). " +
	"Have you made any edits yet? If not, you should have enough context — start editing NOW. " +
	"List what you plan to change and begin immediately. Do not read more files."

// TokenCalibration tracks the ratio between estimated and actual token counts
// to improve the accuracy of estimateTokens over time.
type TokenCalibration struct {
	mu          sync.Mutex
	totalEst    int64
	totalActual int64
	samples     int
}

// NewTokenCalibration creates a new calibration tracker.
func NewTokenCalibration() *TokenCalibration {
	return &TokenCalibration{}
}

// Record adds a sample of estimated vs actual token count.
func (tc *TokenCalibration) Record(estimated, actual int) {
	if tc == nil || actual <= 0 {
		return
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.totalEst += int64(estimated)
	tc.totalActual += int64(actual)
	tc.samples++
}

// CorrectionFactor returns the ratio actual/estimated.
// Returns 1.0 if no samples recorded yet.
func (tc *TokenCalibration) CorrectionFactor() float64 {
	if tc == nil {
		return 1.0
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.samples == 0 || tc.totalEst == 0 {
		return 1.0
	}
	return float64(tc.totalActual) / float64(tc.totalEst)
}

// CalibratedEstimate returns an adjusted token estimate.
func (tc *TokenCalibration) CalibratedEstimate(rawEstimate int) int {
	factor := tc.CorrectionFactor()
	return int(float64(rawEstimate) * factor)
}

// Samples returns the number of calibration samples recorded.
func (tc *TokenCalibration) Samples() int {
	if tc == nil {
		return 0
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.samples
}

// BudgetQuerier is satisfied by *metrics.Metrics and allows testing
// without importing the full metrics package.
type BudgetQuerier interface {
	BudgetUsedPct() float64
	MaxCostValue() float64
}

// AdaptiveCompactionThreshold returns a context token threshold that
// tightens as the cost budget is consumed. This ensures agents operating
// near their budget limit don't waste tokens on stale context.
func AdaptiveCompactionThreshold(m BudgetQuerier) int {
	if m == nil {
		return contextTokenThreshold // 50K default
	}
	if m.MaxCostValue() <= 0 {
		return contextTokenThreshold
	}
	pct := m.BudgetUsedPct()
	switch {
	case pct >= 0.75:
		return 30_000 // aggressive — agent should be finishing
	case pct >= 0.50:
		return 40_000 // moderate — agent should be wrapping up
	default:
		return contextTokenThreshold // 50K — normal operation
	}
}

// MessageRelevance pairs a message index with its relevance score.
type MessageRelevance struct {
	Index int
	Score int
}

// ScoreMessages assigns relevance scores to compactable messages based on
// which files have been edited and which files are referenced in recent context.
func ScoreMessages(
	middle []llms.MessageContent,
	editedFiles map[string]bool,
	recentFiles map[string]bool,
) []MessageRelevance {
	scores := make([]MessageRelevance, len(middle))
	for i, msg := range middle {
		scores[i] = MessageRelevance{Index: i, Score: scoreMessage(msg, editedFiles, recentFiles)}
	}
	return scores
}

func scoreMessage(msg llms.MessageContent, editedFiles, recentFiles map[string]bool) int {
	score := 0
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case llms.ToolCall:
			if p.FunctionCall == nil {
				continue
			}
			path := extractJSONField(p.FunctionCall.Arguments, "path")
			if isEditTool(p.FunctionCall.Name) {
				score += 80
			}
			switch {
			case editedFiles[path]:
				score += 60
			case recentFiles[path]:
				score += 40
			case path != "":
				score += 10
			}
		case llms.ToolCallResponse:
			if strings.HasPrefix(p.Content, "updated ") || strings.HasPrefix(p.Content, "wrote ") {
				score += 50
			}
		}
	}
	return score
}

// ExtractRecentFiles scans messages for file paths mentioned in tool calls.
func ExtractRecentFiles(msgs []llms.MessageContent) map[string]bool {
	files := make(map[string]bool)
	for _, msg := range msgs {
		for _, part := range msg.Parts {
			if tc, ok := part.(llms.ToolCall); ok && tc.FunctionCall != nil {
				if path := extractJSONField(tc.FunctionCall.Arguments, "path"); path != "" {
					files[path] = true
				}
			}
		}
	}
	return files
}

// CollectEditedFiles scans messages for successful Edit/Write tool calls
// and returns a set of edited file paths.
func CollectEditedFiles(messages []llms.MessageContent) map[string]bool {
	edited := make(map[string]bool)
	for _, msg := range messages {
		for _, part := range msg.Parts {
			tc, ok := part.(llms.ToolCall)
			if !ok || tc.FunctionCall == nil {
				continue
			}
			if isEditTool(tc.FunctionCall.Name) {
				if path := extractJSONField(tc.FunctionCall.Arguments, "path"); path != "" {
					edited[path] = true
				}
			}
		}
	}
	return edited
}

// GenerateFileSummary produces a brief summary of a file's top-level
// declarations (func, type, class, def, fn). The summary is stored in the
// read cache and included in compaction summaries so the model can write
// edits without re-reading the file.
func GenerateFileSummary(content string) string {
	lines := strings.Split(content, "\n")
	var decls []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		var name string
		switch {
		case strings.HasPrefix(trimmed, "func "):
			name = extractDeclIdent(trimmed, "func ")
		case strings.HasPrefix(trimmed, "def "):
			name = extractDeclIdent(trimmed, "def ")
		case strings.HasPrefix(trimmed, "class "):
			name = extractDeclIdent(trimmed, "class ")
		case strings.HasPrefix(trimmed, "type "):
			name = extractDeclIdent(trimmed, "type ")
		case strings.HasPrefix(trimmed, "pub fn "):
			name = extractDeclIdent(trimmed, "pub fn ")
		case strings.HasPrefix(trimmed, "fn "):
			name = extractDeclIdent(trimmed, "fn ")
		}
		if name != "" {
			decls = append(decls, name)
		}
		if len(decls) >= 15 {
			break
		}
	}
	if len(decls) == 0 {
		return ""
	}
	result := "declares: " + strings.Join(decls, ", ")
	return TruncateString(result, 200)
}

// extractDeclIdent extracts the first identifier after a declaration keyword.
// Handles Go method receivers: "func (t *Type) Name(" → "Name".
func extractDeclIdent(line, prefix string) string {
	rest := line[len(prefix):]
	// Skip Go method receiver: "(anything) "
	if strings.HasPrefix(rest, "(") {
		end := strings.IndexByte(rest, ')')
		if end >= 0 && end+1 < len(rest) {
			rest = strings.TrimSpace(rest[end+1:])
		}
	}
	var name strings.Builder
	for _, c := range rest {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			name.WriteRune(c)
		} else {
			break
		}
	}
	return name.String()
}

func logReadCacheHit(ctx context.Context, path string, entry ReadCacheEntry) {
	logging.InfoContext(ctx, "  → Read %s [CACHE HIT — unchanged since iteration %d, %d lines, %d bytes]",
		path, entry.Iteration, entry.Lines, entry.Bytes)
}
