import type { ApiErrorEnvelope } from "./chat-models.js";

export type ApiErrorShape = ApiErrorEnvelope;

export type {
  AgentStreamEvent,
  AgentStreamEventMeta,
  AgentToolCallPayload,
  AgentToolResultPayload,
  ChatHistoryResponse,
  ChatSpec,
  DeleteResult,
  RuntimeContent,
  RuntimeMessage,
} from "./chat-models.js";
