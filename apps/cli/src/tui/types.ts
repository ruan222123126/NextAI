import type { Locale } from "../i18n.js";
import type {
  AgentStreamEvent as ContractAgentStreamEvent,
  ChatHistoryResponse as ContractChatHistoryResponse,
  ChatSpec as ContractChatSpec,
  RuntimeContent as ContractRuntimeContent,
  RuntimeMessage as ContractRuntimeMessage,
} from "@nextai/sdk-ts";

export type ChatSpec = ContractChatSpec;

export type RuntimeContent = ContractRuntimeContent;

export type RuntimeMessage = ContractRuntimeMessage;

export type ChatHistoryResponse = ContractChatHistoryResponse;

export type AgentStreamEvent = ContractAgentStreamEvent;

export interface TUIMessage {
  role: "user" | "assistant";
  text: string;
  pending?: boolean;
  failed?: boolean;
}

export interface TUISettings {
  apiBase: string;
  apiKey: string;
  userID: string;
  channel: string;
  locale: Locale;
}

export interface TUIBootstrapOptions {
  sessionID?: string;
  userID: string;
  channel: string;
  apiBase: string;
  apiKey: string;
  locale: Locale;
}
