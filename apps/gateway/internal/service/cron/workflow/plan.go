package workflow

import (
	"errors"
	"fmt"
	"strings"

	"nextai/apps/gateway/internal/domain"
)

const (
	WorkflowVersionV1 = "v1"
	NodeTypeTextEvent = "text_event"
	NodeTypeDelay     = "delay"
	NodeTypeIfEvent   = "if_event"
)

type Plan struct {
	Workflow domain.CronWorkflowSpec
	StartID  string
	NodeByID map[string]domain.CronWorkflowNode
	NextByID map[string]string
	Order    []domain.CronWorkflowNode
}

func BuildPlan(
	spec *domain.CronWorkflowSpec,
	supportsNodeType func(nodeType string) bool,
	validateIfCondition func(raw string) error,
) (*Plan, error) {
	if spec == nil {
		return nil, errors.New("workflow is required for task_type=workflow")
	}

	version := strings.ToLower(strings.TrimSpace(spec.Version))
	if version != WorkflowVersionV1 {
		return nil, fmt.Errorf("unsupported workflow version=%q", spec.Version)
	}
	if len(spec.Nodes) < 2 {
		return nil, errors.New("workflow requires at least 2 nodes")
	}
	if len(spec.Edges) < 1 {
		return nil, errors.New("workflow requires at least 1 edge")
	}

	nodeByID := make(map[string]domain.CronWorkflowNode, len(spec.Nodes))
	normalizedNodes := make([]domain.CronWorkflowNode, 0, len(spec.Nodes))
	startID := ""

	for _, rawNode := range spec.Nodes {
		node := rawNode
		node.ID = strings.TrimSpace(node.ID)
		node.Type = strings.ToLower(strings.TrimSpace(node.Type))
		node.Title = strings.TrimSpace(node.Title)
		node.Text = strings.TrimSpace(node.Text)
		node.IfCondition = strings.TrimSpace(node.IfCondition)

		if node.ID == "" {
			return nil, errors.New("workflow node id is required")
		}
		if _, exists := nodeByID[node.ID]; exists {
			return nil, fmt.Errorf("workflow node id duplicated: %s", node.ID)
		}
		if node.Type != NodeTypeStart && supportsNodeType != nil && !supportsNodeType(node.Type) {
			return nil, fmt.Errorf("workflow node %s has unsupported type=%q", node.ID, node.Type)
		}

		switch node.Type {
		case NodeTypeStart:
			node.ContinueOnError = false
			node.DelaySeconds = 0
			node.Text = ""
			node.IfCondition = ""
			if startID != "" {
				return nil, errors.New("workflow requires exactly one start node")
			}
			startID = node.ID
		case NodeTypeTextEvent:
			node.DelaySeconds = 0
			node.IfCondition = ""
			if node.Text == "" {
				return nil, fmt.Errorf("workflow node %s requires non-empty text", node.ID)
			}
		case NodeTypeDelay:
			node.Text = ""
			node.IfCondition = ""
			if node.DelaySeconds < 0 {
				return nil, fmt.Errorf("workflow node %s delay_seconds must be greater than or equal to 0", node.ID)
			}
		case NodeTypeIfEvent:
			node.Text = ""
			node.DelaySeconds = 0
			if validateIfCondition != nil {
				if err := validateIfCondition(node.IfCondition); err != nil {
					return nil, fmt.Errorf("workflow node %s if_condition invalid: %w", node.ID, err)
				}
			}
		default:
			if supportsNodeType == nil {
				return nil, fmt.Errorf("workflow node %s has unsupported type=%q", node.ID, node.Type)
			}
		}

		nodeByID[node.ID] = node
		normalizedNodes = append(normalizedNodes, node)
	}

	if startID == "" {
		return nil, errors.New("workflow requires exactly one start node")
	}

	edgeIDSet := map[string]struct{}{}
	nextByID := map[string]string{}
	inDegree := map[string]int{}
	outDegree := map[string]int{}
	normalizedEdges := make([]domain.CronWorkflowEdge, 0, len(spec.Edges))

	for _, rawEdge := range spec.Edges {
		edge := rawEdge
		edge.ID = strings.TrimSpace(edge.ID)
		edge.Source = strings.TrimSpace(edge.Source)
		edge.Target = strings.TrimSpace(edge.Target)

		if edge.ID == "" {
			return nil, errors.New("workflow edge id is required")
		}
		if _, exists := edgeIDSet[edge.ID]; exists {
			return nil, fmt.Errorf("workflow edge id duplicated: %s", edge.ID)
		}
		edgeIDSet[edge.ID] = struct{}{}

		if edge.Source == "" || edge.Target == "" {
			return nil, fmt.Errorf("workflow edge %s requires source and target", edge.ID)
		}
		if edge.Source == edge.Target {
			return nil, fmt.Errorf("workflow edge %s cannot link node to itself", edge.ID)
		}
		if _, ok := nodeByID[edge.Source]; !ok {
			return nil, fmt.Errorf("workflow edge %s source not found: %s", edge.ID, edge.Source)
		}
		if _, ok := nodeByID[edge.Target]; !ok {
			return nil, fmt.Errorf("workflow edge %s target not found: %s", edge.ID, edge.Target)
		}

		outDegree[edge.Source]++
		if outDegree[edge.Source] > 1 {
			return nil, fmt.Errorf("workflow node %s has more than one outgoing edge", edge.Source)
		}
		inDegree[edge.Target]++
		if inDegree[edge.Target] > 1 {
			return nil, fmt.Errorf("workflow node %s has more than one incoming edge", edge.Target)
		}
		nextByID[edge.Source] = edge.Target
		normalizedEdges = append(normalizedEdges, edge)
	}

	if inDegree[startID] > 0 {
		return nil, errors.New("workflow start node cannot have incoming edge")
	}
	if outDegree[startID] == 0 {
		return nil, errors.New("workflow start node must connect to at least one executable node")
	}

	reachable := map[string]bool{startID: true}
	order := make([]domain.CronWorkflowNode, 0, len(nodeByID)-1)
	cursor := startID
	for {
		nextID, ok := nextByID[cursor]
		if !ok {
			break
		}
		if reachable[nextID] {
			return nil, errors.New("workflow graph must be acyclic")
		}
		reachable[nextID] = true
		nextNode := nodeByID[nextID]
		if nextNode.Type == NodeTypeStart {
			return nil, errors.New("workflow start node cannot be targeted by execution path")
		}
		order = append(order, nextNode)
		cursor = nextID
	}

	if len(order) == 0 {
		return nil, errors.New("workflow requires at least one executable node")
	}
	for nodeID, node := range nodeByID {
		if node.Type == NodeTypeStart {
			continue
		}
		if !reachable[nodeID] {
			return nil, fmt.Errorf("workflow node %s is not reachable from start", nodeID)
		}
	}

	var viewport *domain.CronWorkflowViewport
	if spec.Viewport != nil {
		v := *spec.Viewport
		if v.Zoom <= 0 {
			v.Zoom = 1
		}
		viewport = &v
	}

	return &Plan{
		Workflow: domain.CronWorkflowSpec{
			Version:  WorkflowVersionV1,
			Viewport: viewport,
			Nodes:    normalizedNodes,
			Edges:    normalizedEdges,
		},
		StartID:  startID,
		NodeByID: nodeByID,
		NextByID: nextByID,
		Order:    order,
	}, nil
}
