package metrics

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cowdogmoo/squad/agent"
)

// EstimateNode represents one agent in a cost estimate tree.
type EstimateNode struct {
	Agent               string
	EstimatedIters      int
	InputTokensPerIter  int64
	OutputTokensPerIter int64
	InputCost           float64
	OutputCost          float64
	TotalCost           float64
	Children            []*EstimateNode
}

// TotalTreeCost returns the sum of this node's cost and all descendant costs.
func (n *EstimateNode) TotalTreeCost() float64 {
	total := n.TotalCost
	for _, c := range n.Children {
		total += c.TotalTreeCost()
	}
	return total
}

// defaultIterations is the fallback when an agent has no budget hints.
const defaultIterations = 25

// defaultInputPerIter is a rough average of input tokens per iteration.
const defaultInputPerIter int64 = 6000

// defaultOutputPerIter is a rough average of output tokens per iteration.
const defaultOutputPerIter int64 = 1500

// orchestratorOutputPerIter is higher because Task tool prompts are large.
const orchestratorOutputPerIter int64 = 3000

// EstimateCost walks the agent graph starting from rootAgent and returns
// a cost estimate tree for the given provider and model.
func EstimateCost(agentsDir, rootAgent, provider, model string) (*EstimateNode, error) {
	return estimateAgent(agentsDir, rootAgent, provider, model, 0)
}

func estimateAgent(agentsDir, agentName, provider, model string, depth int) (*EstimateNode, error) {
	if depth > 5 {
		return nil, fmt.Errorf("agent graph too deep (>5), possible cycle at %s", agentName)
	}

	agentPath := filepath.Join(agentsDir, agentName)
	manifest, err := agent.LoadManifest(agentPath)
	if err != nil {
		// If we can't load the manifest, return a default estimate
		return defaultEstimate(agentName, provider, model, false), nil
	}

	budget := manifest.Budget
	iters := defaultIterations
	isOrchestrator := false
	var childNames []string

	if budget != nil {
		if budget.EstimatedIterations > 0 {
			iters = budget.EstimatedIterations
		}
		childNames = budget.ChildNames()
		isOrchestrator = len(childNames) > 0
	}

	node := buildEstimateNode(agentName, provider, model, iters, isOrchestrator)

	// Recurse into children
	for _, childName := range childNames {
		childNode, err := estimateAgent(agentsDir, childName, provider, model, depth+1)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, childNode)
	}

	return node, nil
}

func buildEstimateNode(agentName, provider, model string, iters int, isOrchestrator bool) *EstimateNode {
	pricing := GetPricing(provider, model)

	inputPerIter := defaultInputPerIter
	outputPerIter := defaultOutputPerIter
	if isOrchestrator {
		outputPerIter = orchestratorOutputPerIter
	}

	totalInput := int64(iters) * inputPerIter
	totalOutput := int64(iters) * outputPerIter
	inputCost := float64(totalInput) / 1_000_000 * pricing.InputPerMillion
	outputCost := float64(totalOutput) / 1_000_000 * pricing.OutputPerMillion

	return &EstimateNode{
		Agent:               agentName,
		EstimatedIters:      iters,
		InputTokensPerIter:  inputPerIter,
		OutputTokensPerIter: outputPerIter,
		InputCost:           inputCost,
		OutputCost:          outputCost,
		TotalCost:           inputCost + outputCost,
	}
}

func defaultEstimate(agentName, provider, model string, isOrchestrator bool) *EstimateNode {
	return buildEstimateNode(agentName, provider, model, defaultIterations, isOrchestrator)
}

// FormatEstimate renders a cost estimate tree as a human-readable string.
func FormatEstimate(root *EstimateNode, provider, model string) string {
	var sb strings.Builder
	pricing := GetPricing(provider, model)

	fmt.Fprintf(&sb, "Cost Estimate (%s / %s)\n", provider, model)
	fmt.Fprintf(&sb, "Pricing: $%.2f / 1M input, $%.2f / 1M output\n", pricing.InputPerMillion, pricing.OutputPerMillion)
	sb.WriteString("──────────────────────────────────────────\n")

	formatNode(&sb, root, 0)

	sb.WriteString("──────────────────────────────────────────\n")
	total := root.TotalTreeCost()
	fmt.Fprintf(&sb, "Estimated total: $%.2f – $%.2f\n", total*0.6, total*1.5)
	fmt.Fprintf(&sb, "  (range accounts for codebase size variance)\n")

	return sb.String()
}

func formatNode(sb *strings.Builder, node *EstimateNode, indent int) {
	prefix := strings.Repeat("  ", indent)
	fmt.Fprintf(sb, "%s%-20s ~%d iters  $%.4f\n", prefix, node.Agent, node.EstimatedIters, node.TotalCost)
	for _, child := range node.Children {
		formatNode(sb, child, indent+1)
	}
}
