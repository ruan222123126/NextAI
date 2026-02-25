package workflow

import (
	"context"
	"strings"

	"nextai/apps/gateway/internal/domain"
)

const (
	NodeTypeStart = "start"
)

type NodeResult struct {
	Stop bool
}

type NodeHandler interface {
	Type() string
	Execute(ctx context.Context, job domain.CronJobSpec, node domain.CronWorkflowNode) (NodeResult, error)
}

type NodeRegistry struct {
	handlers map[string]NodeHandler
}

func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{handlers: map[string]NodeHandler{}}
}

func (r *NodeRegistry) Register(handler NodeHandler) {
	if r == nil || handler == nil {
		return
	}
	nodeType := normalizeNodeType(handler.Type())
	if nodeType == "" || nodeType == NodeTypeStart {
		return
	}
	if r.handlers == nil {
		r.handlers = map[string]NodeHandler{}
	}
	r.handlers[nodeType] = handler
}

func (r *NodeRegistry) Resolve(nodeType string) (NodeHandler, bool) {
	if r == nil {
		return nil, false
	}
	handler, ok := r.handlers[normalizeNodeType(nodeType)]
	return handler, ok
}

func (r *NodeRegistry) Supports(nodeType string) bool {
	if normalizeNodeType(nodeType) == NodeTypeStart {
		return true
	}
	_, ok := r.Resolve(nodeType)
	return ok
}

func normalizeNodeType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
