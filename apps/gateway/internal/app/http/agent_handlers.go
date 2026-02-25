package transport

import (
	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
)

type AgentHandlers struct {
	ListChats             stdhttp.HandlerFunc
	CreateChat            stdhttp.HandlerFunc
	BatchDeleteChats      stdhttp.HandlerFunc
	GetChat               stdhttp.HandlerFunc
	UpdateChat            stdhttp.HandlerFunc
	DeleteChat            stdhttp.HandlerFunc
	ProcessAgent          stdhttp.HandlerFunc
	GetAgentSystemLayers  stdhttp.HandlerFunc
	TogglePlanMode        stdhttp.HandlerFunc
	CompilePlan           stdhttp.HandlerFunc
	SubmitPlanClarify     stdhttp.HandlerFunc
	RevisePlan            stdhttp.HandlerFunc
	ExecutePlan           stdhttp.HandlerFunc
	GetPlan               stdhttp.HandlerFunc
	BootstrapSession      stdhttp.HandlerFunc
	SetSessionModel       stdhttp.HandlerFunc
	PreviewMutation       stdhttp.HandlerFunc
	ApplyMutation         stdhttp.HandlerFunc
	SubmitToolInputAnswer stdhttp.HandlerFunc
	ProcessQQInbound      stdhttp.HandlerFunc
	GetQQInboundState     stdhttp.HandlerFunc
}

func registerAgentRoutes(api chi.Router, handlers AgentHandlers) {
	api.Route("/chats", func(r chi.Router) {
		r.Get("/", mustHandler("list-chats", handlers.ListChats))
		r.Post("/", mustHandler("create-chat", handlers.CreateChat))
		r.Post("/batch-delete", mustHandler("batch-delete-chats", handlers.BatchDeleteChats))
		r.Get("/{chat_id}", mustHandler("get-chat", handlers.GetChat))
		r.Put("/{chat_id}", mustHandler("update-chat", handlers.UpdateChat))
		r.Delete("/{chat_id}", mustHandler("delete-chat", handlers.DeleteChat))
	})

	api.Post("/agent/process", mustHandler("process-agent", handlers.ProcessAgent))
	api.Get("/agent/system-layers", mustHandler("get-agent-system-layers", handlers.GetAgentSystemLayers))
	api.Post("/agent/plan/toggle", mustHandler("toggle-plan-mode", handlers.TogglePlanMode))
	api.Post("/agent/plan/compile", mustHandler("compile-plan", handlers.CompilePlan))
	api.Post("/agent/plan/clarify/answer", mustHandler("submit-plan-clarify", handlers.SubmitPlanClarify))
	api.Post("/agent/plan/revise", mustHandler("revise-plan", handlers.RevisePlan))
	api.Post("/agent/plan/execute", mustHandler("execute-plan", handlers.ExecutePlan))
	api.Get("/agent/plan/{chat_id}", mustHandler("get-plan", handlers.GetPlan))
	api.Post("/agent/self/sessions/bootstrap", mustHandler("selfops-bootstrap-session", handlers.BootstrapSession))
	api.Put("/agent/self/sessions/{session_id}/model", mustHandler("selfops-set-session-model", handlers.SetSessionModel))
	api.Post("/agent/self/config-mutations/preview", mustHandler("selfops-preview-mutation", handlers.PreviewMutation))
	api.Post("/agent/self/config-mutations/apply", mustHandler("selfops-apply-mutation", handlers.ApplyMutation))
	api.Post("/agent/tool-input-answer", mustHandler("agent-tool-input-answer", handlers.SubmitToolInputAnswer))
	api.Post("/channels/qq/inbound", mustHandler("process-qq-inbound", handlers.ProcessQQInbound))
	api.Get("/channels/qq/state", mustHandler("get-qq-inbound-state", handlers.GetQQInboundState))
}
