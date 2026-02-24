import { readFile } from "node:fs/promises";
import process from "node:process";
import { parse } from "yaml";

const specPath = new URL("../../packages/contracts/openapi/openapi.yaml", import.meta.url);

const raw = await readFile(specPath, "utf8");
const spec = parse(raw);
const failures = [];

function expect(condition, message) {
  if (!condition) {
    failures.push(message);
  }
}

function hasRequired(schema, key) {
  return Array.isArray(schema?.required) && schema.required.includes(key);
}

const schemas = spec?.components?.schemas ?? {};
const chatSpec = schemas.ChatSpec;
const runtimeContent = schemas.RuntimeContent;
const agentInputMessage = schemas.AgentInputMessage;
const agentProcessRequest = schemas.AgentProcessRequest;
const cronScheduleSpec = schemas.CronScheduleSpec;
const cronDispatchTarget = schemas.CronDispatchTarget;
const cronRuntimeSpec = schemas.CronRuntimeSpec;
const cronJobSpec = schemas.CronJobSpec;
const cronJobState = schemas.CronJobState;
const cronJobView = schemas.CronJobView;
const cronWorkflowSpec = schemas.CronWorkflowSpec;
const cronWorkflowNode = schemas.CronWorkflowNode;
const cronWorkflowEdge = schemas.CronWorkflowEdge;
const cronWorkflowViewport = schemas.CronWorkflowViewport;
const cronWorkflowExecution = schemas.CronWorkflowExecution;
const cronWorkflowNodeExecution = schemas.CronWorkflowNodeExecution;
const modelSlotConfig = schemas.ModelSlotConfig;
const activeModelsInfo = schemas.ActiveModelsInfo;
const modelInfo = schemas.ModelInfo;
const providerInfo = schemas.ProviderInfo;
const providerTypeInfo = schemas.ProviderTypeInfo;
const providerConfigPatch = schemas.ProviderConfigPatch;
const deleteResult = schemas.DeleteResult;
const modelCatalogInfo = schemas.ModelCatalogInfo;
const selfBootstrapRequest = schemas.SelfBootstrapRequest;
const selfBootstrapResponse = schemas.SelfBootstrapResponse;
const selfSessionModelRequest = schemas.SelfSessionModelRequest;
const selfSessionModelResponse = schemas.SelfSessionModelResponse;
const selfMutationPreviewRequest = schemas.SelfMutationPreviewRequest;
const selfMutationPreviewResponse = schemas.SelfMutationPreviewResponse;
const selfMutationApplyRequest = schemas.SelfMutationApplyRequest;
const selfMutationApplyResponse = schemas.SelfMutationApplyResponse;
const selfMutationOperation = schemas.SelfMutationOperation;
const selfMutationChecks = schemas.SelfMutationChecks;
const apiKeyAuth = spec?.components?.securitySchemes?.ApiKeyAuth;

expect(spec?.openapi === "3.0.3", "openapi 版本必须是 3.0.3");
expect(typeof spec?.paths === "object", "paths 必须存在");

expect(chatSpec?.properties?.created_at?.format === "date-time", "ChatSpec.created_at 必须声明 date-time format");
expect(chatSpec?.properties?.updated_at?.format === "date-time", "ChatSpec.updated_at 必须声明 date-time format");
expect(hasRequired(chatSpec, "session_id"), "ChatSpec.required 必须包含 session_id");
expect(hasRequired(chatSpec, "user_id"), "ChatSpec.required 必须包含 user_id");
expect(hasRequired(chatSpec, "channel"), "ChatSpec.required 必须包含 channel");

expect(Array.isArray(runtimeContent?.properties?.type?.enum), "RuntimeContent.type 必须声明 enum");
expect(runtimeContent?.properties?.type?.enum?.includes("text"), "RuntimeContent.type enum 必须包含 text");
expect(hasRequired(runtimeContent, "type"), "RuntimeContent.required 必须包含 type");

expect(Array.isArray(agentInputMessage?.properties?.role?.enum), "AgentInputMessage.role 必须声明 enum");
expect(agentInputMessage?.properties?.role?.enum?.includes("user"), "AgentInputMessage.role enum 必须包含 user");
expect(agentInputMessage?.properties?.role?.enum?.includes("assistant"), "AgentInputMessage.role enum 必须包含 assistant");
expect(agentInputMessage?.properties?.type?.enum?.includes("message"), "AgentInputMessage.type enum 必须包含 message");
expect(agentInputMessage?.properties?.content?.minItems === 1, "AgentInputMessage.content 必须设置 minItems=1");
expect(hasRequired(agentInputMessage, "role"), "AgentInputMessage.required 必须包含 role");
expect(hasRequired(agentInputMessage, "type"), "AgentInputMessage.required 必须包含 type");
expect(hasRequired(agentInputMessage, "content"), "AgentInputMessage.required 必须包含 content");

expect(agentProcessRequest?.properties?.input?.minItems === 1, "AgentProcessRequest.input 必须设置 minItems=1");
expect(hasRequired(agentProcessRequest, "input"), "AgentProcessRequest.required 必须包含 input");
expect(hasRequired(agentProcessRequest, "session_id"), "AgentProcessRequest.required 必须包含 session_id");
expect(hasRequired(agentProcessRequest, "user_id"), "AgentProcessRequest.required 必须包含 user_id");
expect(agentProcessRequest?.properties?.channel?.minLength === 1, "AgentProcessRequest.channel 必须设置 minLength=1");
expect(hasRequired(agentProcessRequest, "stream"), "AgentProcessRequest.required 必须包含 stream");

expect(Array.isArray(cronScheduleSpec?.properties?.type?.enum), "CronScheduleSpec.type 必须声明 enum");
expect(cronScheduleSpec?.properties?.type?.enum?.includes("interval"), "CronScheduleSpec.type enum 必须包含 interval");
expect(cronScheduleSpec?.properties?.type?.enum?.includes("cron"), "CronScheduleSpec.type enum 必须包含 cron");
expect(hasRequired(cronScheduleSpec, "cron"), "CronScheduleSpec.required 必须包含 cron");

expect(hasRequired(cronDispatchTarget, "user_id"), "CronDispatchTarget.required 必须包含 user_id");
expect(hasRequired(cronDispatchTarget, "session_id"), "CronDispatchTarget.required 必须包含 session_id");

expect(cronRuntimeSpec?.properties?.max_concurrency?.minimum === 1, "CronRuntimeSpec.max_concurrency 必须设置 minimum=1");
expect(cronRuntimeSpec?.properties?.timeout_seconds?.minimum === 1, "CronRuntimeSpec.timeout_seconds 必须设置 minimum=1");
expect(cronRuntimeSpec?.properties?.misfire_grace_seconds?.minimum === 0, "CronRuntimeSpec.misfire_grace_seconds 必须设置 minimum=0");

expect(hasRequired(cronJobSpec, "schedule"), "CronJobSpec.required 必须包含 schedule");
expect(hasRequired(cronJobSpec, "task_type"), "CronJobSpec.required 必须包含 task_type");
expect(hasRequired(cronJobSpec, "dispatch"), "CronJobSpec.required 必须包含 dispatch");
expect(hasRequired(cronJobSpec, "runtime"), "CronJobSpec.required 必须包含 runtime");
expect(Array.isArray(cronJobSpec?.properties?.task_type?.enum), "CronJobSpec.task_type 必须声明 enum");
expect(cronJobSpec?.properties?.task_type?.enum?.includes("text"), "CronJobSpec.task_type enum 必须包含 text");
expect(cronJobSpec?.properties?.task_type?.enum?.includes("workflow"), "CronJobSpec.task_type enum 必须包含 workflow");
expect(cronJobSpec?.properties?.workflow?.$ref === "#/components/schemas/CronWorkflowSpec", "CronJobSpec.workflow 必须引用 CronWorkflowSpec");

expect(Array.isArray(cronJobState?.properties?.last_status?.enum), "CronJobState.last_status 必须声明 enum");
expect(cronJobState?.properties?.last_status?.enum?.includes("succeeded"), "CronJobState.last_status enum 必须包含 succeeded");
expect(cronJobState?.properties?.paused?.type === "boolean", "CronJobState.paused 必须是 boolean");
expect(cronJobState?.properties?.last_execution?.$ref === "#/components/schemas/CronWorkflowExecution", "CronJobState.last_execution 必须引用 CronWorkflowExecution");
expect(hasRequired(cronJobView, "spec"), "CronJobView.required 必须包含 spec");
expect(hasRequired(cronJobView, "state"), "CronJobView.required 必须包含 state");

expect(hasRequired(cronWorkflowSpec, "version"), "CronWorkflowSpec.required 必须包含 version");
expect(hasRequired(cronWorkflowSpec, "nodes"), "CronWorkflowSpec.required 必须包含 nodes");
expect(hasRequired(cronWorkflowSpec, "edges"), "CronWorkflowSpec.required 必须包含 edges");
expect(cronWorkflowSpec?.properties?.nodes?.minItems === 2, "CronWorkflowSpec.nodes 必须设置 minItems=2");
expect(cronWorkflowSpec?.properties?.edges?.minItems === 1, "CronWorkflowSpec.edges 必须设置 minItems=1");
expect(Array.isArray(cronWorkflowNode?.properties?.type?.enum), "CronWorkflowNode.type 必须声明 enum");
expect(cronWorkflowNode?.properties?.type?.enum?.includes("start"), "CronWorkflowNode.type enum 必须包含 start");
expect(cronWorkflowNode?.properties?.type?.enum?.includes("text_event"), "CronWorkflowNode.type enum 必须包含 text_event");
expect(cronWorkflowNode?.properties?.type?.enum?.includes("delay"), "CronWorkflowNode.type enum 必须包含 delay");
expect(cronWorkflowNode?.properties?.type?.enum?.includes("if_event"), "CronWorkflowNode.type enum 必须包含 if_event");
expect(cronWorkflowNode?.properties?.delay_seconds?.minimum === 0, "CronWorkflowNode.delay_seconds 必须设置 minimum=0");
expect(cronWorkflowEdge?.properties?.source?.minLength === 1, "CronWorkflowEdge.source 必须设置 minLength=1");
expect(cronWorkflowEdge?.properties?.target?.minLength === 1, "CronWorkflowEdge.target 必须设置 minLength=1");
expect(cronWorkflowViewport?.properties?.zoom?.default === 1, "CronWorkflowViewport.zoom 默认值必须为 1.0");
expect(hasRequired(cronWorkflowExecution, "run_id"), "CronWorkflowExecution.required 必须包含 run_id");
expect(hasRequired(cronWorkflowExecution, "started_at"), "CronWorkflowExecution.required 必须包含 started_at");
expect(hasRequired(cronWorkflowExecution, "had_failures"), "CronWorkflowExecution.required 必须包含 had_failures");
expect(hasRequired(cronWorkflowExecution, "nodes"), "CronWorkflowExecution.required 必须包含 nodes");
expect(cronWorkflowNodeExecution?.properties?.status?.enum?.includes("failed"), "CronWorkflowNodeExecution.status enum 必须包含 failed");
expect(cronWorkflowNodeExecution?.properties?.node_type?.enum?.includes("delay"), "CronWorkflowNodeExecution.node_type enum 必须包含 delay");
expect(cronWorkflowNodeExecution?.properties?.node_type?.enum?.includes("if_event"), "CronWorkflowNodeExecution.node_type enum 必须包含 if_event");

expect(modelSlotConfig?.properties?.provider_id?.minLength === 1, "ModelSlotConfig.provider_id 必须设置 minLength=1");
expect(modelSlotConfig?.properties?.model?.minLength === 1, "ModelSlotConfig.model 必须设置 minLength=1");
expect(hasRequired(modelSlotConfig, "provider_id"), "ModelSlotConfig.required 必须包含 provider_id");
expect(hasRequired(modelSlotConfig, "model"), "ModelSlotConfig.required 必须包含 model");
expect(hasRequired(activeModelsInfo, "active_llm"), "ActiveModelsInfo.required 必须包含 active_llm");
expect(hasRequired(modelInfo, "id"), "ModelInfo.required 必须包含 id");
expect(hasRequired(modelInfo, "name"), "ModelInfo.required 必须包含 name");
expect(providerInfo?.properties?.display_name?.minLength === 1, "ProviderInfo.display_name 必须设置 minLength=1");
expect(hasRequired(providerInfo, "display_name"), "ProviderInfo.required 必须包含 display_name");
expect(providerInfo?.properties?.openai_compatible?.type === "boolean", "ProviderInfo.openai_compatible 必须是 boolean");
expect(hasRequired(providerInfo, "openai_compatible"), "ProviderInfo.required 必须包含 openai_compatible");
expect(providerInfo?.properties?.enabled?.type === "boolean", "ProviderInfo.enabled 必须是 boolean");
expect(providerInfo?.properties?.store?.type === "boolean", "ProviderInfo.store 必须是 boolean");
expect(providerInfo?.properties?.reasoning_effort?.type === "string", "ProviderInfo.reasoning_effort 必须是 string");
expect(providerInfo?.properties?.reasoning_effort?.enum?.includes("low"), "ProviderInfo.reasoning_effort enum 必须包含 low");
expect(providerInfo?.properties?.reasoning_effort?.enum?.includes("high"), "ProviderInfo.reasoning_effort enum 必须包含 high");
expect(hasRequired(providerInfo, "models"), "ProviderInfo.required 必须包含 models");
expect(hasRequired(providerTypeInfo, "id"), "ProviderTypeInfo.required 必须包含 id");
expect(hasRequired(providerTypeInfo, "display_name"), "ProviderTypeInfo.required 必须包含 display_name");
expect(providerConfigPatch?.properties?.timeout_ms?.minimum === 0, "ProviderConfigPatch.timeout_ms 必须设置 minimum=0");
expect(providerConfigPatch?.properties?.store?.type === "boolean", "ProviderConfigPatch.store 必须是 boolean");
expect(providerConfigPatch?.properties?.reasoning_effort?.type === "string", "ProviderConfigPatch.reasoning_effort 必须是 string");
expect(providerConfigPatch?.properties?.reasoning_effort?.enum?.includes("medium"), "ProviderConfigPatch.reasoning_effort enum 必须包含 medium");
expect(deleteResult?.properties?.deleted?.type === "boolean", "DeleteResult.deleted 必须是 boolean");
expect(hasRequired(deleteResult, "deleted"), "DeleteResult.required 必须包含 deleted");
expect(hasRequired(modelCatalogInfo, "providers"), "ModelCatalogInfo.required 必须包含 providers");
expect(hasRequired(modelCatalogInfo, "provider_types"), "ModelCatalogInfo.required 必须包含 provider_types");
expect(hasRequired(modelCatalogInfo, "defaults"), "ModelCatalogInfo.required 必须包含 defaults");
expect(hasRequired(modelCatalogInfo, "active_llm"), "ModelCatalogInfo.required 必须包含 active_llm");

expect(hasRequired(selfBootstrapRequest, "user_id"), "SelfBootstrapRequest.required 必须包含 user_id");
expect(hasRequired(selfBootstrapRequest, "first_input"), "SelfBootstrapRequest.required 必须包含 first_input");
expect(hasRequired(selfBootstrapResponse, "chat"), "SelfBootstrapResponse.required 必须包含 chat");
expect(hasRequired(selfBootstrapResponse, "reply"), "SelfBootstrapResponse.required 必须包含 reply");
expect(hasRequired(selfBootstrapResponse, "applied_model"), "SelfBootstrapResponse.required 必须包含 applied_model");
expect(hasRequired(selfSessionModelRequest, "user_id"), "SelfSessionModelRequest.required 必须包含 user_id");
expect(hasRequired(selfSessionModelRequest, "provider_id"), "SelfSessionModelRequest.required 必须包含 provider_id");
expect(hasRequired(selfSessionModelRequest, "model"), "SelfSessionModelRequest.required 必须包含 model");
expect(hasRequired(selfSessionModelResponse, "session_id"), "SelfSessionModelResponse.required 必须包含 session_id");
expect(hasRequired(selfSessionModelResponse, "chat_id"), "SelfSessionModelResponse.required 必须包含 chat_id");
expect(hasRequired(selfSessionModelResponse, "active_llm_override"), "SelfSessionModelResponse.required 必须包含 active_llm_override");
expect(selfMutationPreviewRequest?.properties?.target?.enum?.includes("workspace_file"), "SelfMutationPreviewRequest.target enum 必须包含 workspace_file");
expect(selfMutationPreviewRequest?.properties?.target?.enum?.includes("provider_config"), "SelfMutationPreviewRequest.target enum 必须包含 provider_config");
expect(selfMutationPreviewRequest?.properties?.target?.enum?.includes("active_llm"), "SelfMutationPreviewRequest.target enum 必须包含 active_llm");
expect(selfMutationPreviewRequest?.properties?.operations?.minItems === 1, "SelfMutationPreviewRequest.operations 必须设置 minItems=1");
expect(hasRequired(selfMutationPreviewRequest, "target"), "SelfMutationPreviewRequest.required 必须包含 target");
expect(hasRequired(selfMutationPreviewRequest, "operations"), "SelfMutationPreviewRequest.required 必须包含 operations");
expect(selfMutationOperation?.properties?.kind?.enum?.includes("json_patch"), "SelfMutationOperation.kind enum 必须包含 json_patch");
expect(selfMutationOperation?.properties?.kind?.enum?.includes("text_rewrite"), "SelfMutationOperation.kind enum 必须包含 text_rewrite");
expect(hasRequired(selfMutationPreviewResponse, "mutation_id"), "SelfMutationPreviewResponse.required 必须包含 mutation_id");
expect(hasRequired(selfMutationPreviewResponse, "confirm_hash"), "SelfMutationPreviewResponse.required 必须包含 confirm_hash");
expect(hasRequired(selfMutationPreviewResponse, "checks"), "SelfMutationPreviewResponse.required 必须包含 checks");
expect(hasRequired(selfMutationPreviewResponse, "diff_summary"), "SelfMutationPreviewResponse.required 必须包含 diff_summary");
expect(hasRequired(selfMutationPreviewResponse, "requires_sensitive_allow"), "SelfMutationPreviewResponse.required 必须包含 requires_sensitive_allow");
expect(hasRequired(selfMutationChecks, "path_whitelist_passed"), "SelfMutationChecks.required 必须包含 path_whitelist_passed");
expect(hasRequired(selfMutationChecks, "structure_valid"), "SelfMutationChecks.required 必须包含 structure_valid");
expect(hasRequired(selfMutationChecks, "risk_level"), "SelfMutationChecks.required 必须包含 risk_level");
expect(hasRequired(selfMutationApplyRequest, "mutation_id"), "SelfMutationApplyRequest.required 必须包含 mutation_id");
expect(hasRequired(selfMutationApplyRequest, "confirm_hash"), "SelfMutationApplyRequest.required 必须包含 confirm_hash");
expect(hasRequired(selfMutationApplyResponse, "applied"), "SelfMutationApplyResponse.required 必须包含 applied");
expect(hasRequired(selfMutationApplyResponse, "applied_targets"), "SelfMutationApplyResponse.required 必须包含 applied_targets");
expect(hasRequired(selfMutationApplyResponse, "audit_id"), "SelfMutationApplyResponse.required 必须包含 audit_id");

expect(apiKeyAuth?.type === "apiKey", "必须声明 ApiKeyAuth 安全方案");
expect(apiKeyAuth?.in === "header", "ApiKeyAuth 必须位于 header");
expect(apiKeyAuth?.name === "X-API-Key", "ApiKeyAuth header 必须为 X-API-Key");

if (failures.length > 0) {
  console.error("OpenAPI lint failed:");
  for (const failure of failures) {
    console.error(`- ${failure}`);
  }
  process.exit(1);
}

console.log("OpenAPI lint passed");
