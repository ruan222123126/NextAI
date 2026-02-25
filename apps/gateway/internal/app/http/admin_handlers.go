package transport

import (
	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
)

type AdminHandlers struct {
	Models    AdminModelHandlers
	Envs      AdminEnvHandlers
	Skills    AdminSkillHandlers
	Workspace AdminWorkspaceHandlers
	Config    AdminConfigHandlers
}

type AdminModelHandlers struct {
	ListProviders     stdhttp.HandlerFunc
	GetModelCatalog   stdhttp.HandlerFunc
	ConfigureProvider stdhttp.HandlerFunc
	DeleteProvider    stdhttp.HandlerFunc
	GetActiveModels   stdhttp.HandlerFunc
	SetActiveModels   stdhttp.HandlerFunc
}

type AdminEnvHandlers struct {
	ListEnvs  stdhttp.HandlerFunc
	PutEnvs   stdhttp.HandlerFunc
	DeleteEnv stdhttp.HandlerFunc
}

type AdminSkillHandlers struct {
	ListSkills         stdhttp.HandlerFunc
	ListAvailableSkill stdhttp.HandlerFunc
	BatchDisableSkills stdhttp.HandlerFunc
	BatchEnableSkills  stdhttp.HandlerFunc
	CreateSkill        stdhttp.HandlerFunc
	DisableSkill       stdhttp.HandlerFunc
	EnableSkill        stdhttp.HandlerFunc
	DeleteSkill        stdhttp.HandlerFunc
	LoadSkillFile      stdhttp.HandlerFunc
}

type AdminWorkspaceHandlers struct {
	ListWorkspaceFiles stdhttp.HandlerFunc
	GetWorkspaceFile   stdhttp.HandlerFunc
	PutWorkspaceFile   stdhttp.HandlerFunc
	UploadWorkspace    stdhttp.HandlerFunc
	DeleteWorkspace    stdhttp.HandlerFunc
	ExportWorkspace    stdhttp.HandlerFunc
	ImportWorkspace    stdhttp.HandlerFunc
}

type AdminConfigHandlers struct {
	Channels AdminChannelHandlers
}

type AdminChannelHandlers struct {
	ListChannels     stdhttp.HandlerFunc
	ListChannelTypes stdhttp.HandlerFunc
	PutChannels      stdhttp.HandlerFunc
	GetChannel       stdhttp.HandlerFunc
	PutChannel       stdhttp.HandlerFunc
}

func registerAdminRoutes(api chi.Router, handlers AdminHandlers) {
	api.Route("/models", func(r chi.Router) {
		r.Get("/", mustHandler("list-providers", handlers.Models.ListProviders))
		r.Get("/catalog", mustHandler("get-model-catalog", handlers.Models.GetModelCatalog))
		r.Put("/{provider_id}/config", mustHandler("configure-provider", handlers.Models.ConfigureProvider))
		r.Delete("/{provider_id}", mustHandler("delete-provider", handlers.Models.DeleteProvider))
		r.Get("/active", mustHandler("get-active-models", handlers.Models.GetActiveModels))
		r.Put("/active", mustHandler("set-active-models", handlers.Models.SetActiveModels))
	})

	api.Route("/envs", func(r chi.Router) {
		r.Get("/", mustHandler("list-envs", handlers.Envs.ListEnvs))
		r.Put("/", mustHandler("put-envs", handlers.Envs.PutEnvs))
		r.Delete("/{key}", mustHandler("delete-env", handlers.Envs.DeleteEnv))
	})

	api.Route("/skills", func(r chi.Router) {
		r.Get("/", mustHandler("list-skills", handlers.Skills.ListSkills))
		r.Get("/available", mustHandler("list-available-skills", handlers.Skills.ListAvailableSkill))
		r.Post("/batch-disable", mustHandler("batch-disable-skills", handlers.Skills.BatchDisableSkills))
		r.Post("/batch-enable", mustHandler("batch-enable-skills", handlers.Skills.BatchEnableSkills))
		r.Post("/", mustHandler("create-skill", handlers.Skills.CreateSkill))
		r.Post("/{skill_name}/disable", mustHandler("disable-skill", handlers.Skills.DisableSkill))
		r.Post("/{skill_name}/enable", mustHandler("enable-skill", handlers.Skills.EnableSkill))
		r.Delete("/{skill_name}", mustHandler("delete-skill", handlers.Skills.DeleteSkill))
		r.Get("/{skill_name}/files/{source}/{file_path}", mustHandler("load-skill-file", handlers.Skills.LoadSkillFile))
	})

	api.Route("/workspace", func(r chi.Router) {
		r.Get("/files", mustHandler("list-workspace-files", handlers.Workspace.ListWorkspaceFiles))
		r.Get("/files/*", mustHandler("get-workspace-file", handlers.Workspace.GetWorkspaceFile))
		r.Put("/files/*", mustHandler("put-workspace-file", handlers.Workspace.PutWorkspaceFile))
		r.Post("/uploads", mustHandler("upload-workspace-file", handlers.Workspace.UploadWorkspace))
		r.Delete("/files/*", mustHandler("delete-workspace-file", handlers.Workspace.DeleteWorkspace))
		r.Get("/export", mustHandler("export-workspace", handlers.Workspace.ExportWorkspace))
		r.Post("/import", mustHandler("import-workspace", handlers.Workspace.ImportWorkspace))
	})

	api.Route("/config", func(r chi.Router) {
		r.Get("/channels", mustHandler("list-channels", handlers.Config.Channels.ListChannels))
		r.Get("/channels/types", mustHandler("list-channel-types", handlers.Config.Channels.ListChannelTypes))
		r.Put("/channels", mustHandler("put-channels", handlers.Config.Channels.PutChannels))
		r.Get("/channels/{channel_name}", mustHandler("get-channel", handlers.Config.Channels.GetChannel))
		r.Put("/channels/{channel_name}", mustHandler("put-channel", handlers.Config.Channels.PutChannel))
	})
}
