export interface ChatSpec {
    id: string;
    name: string;
    session_id: string;
    user_id: string;
    channel: string;
    created_at?: string;
    updated_at?: string;
    meta?: Record<string, unknown>;
}
export interface RuntimeContent {
    type?: string;
    text?: string;
}
export interface RuntimeMessage {
    id?: string;
    role?: string;
    content?: RuntimeContent[];
    metadata?: Record<string, unknown>;
}
export interface ChatHistoryResponse {
    messages: RuntimeMessage[];
}
export interface AgentToolCallPayload {
    name?: string;
    input?: Record<string, unknown>;
}
export interface AgentToolResultPayload {
    name?: string;
    ok?: boolean;
    summary?: string;
    output?: string;
    input?: Record<string, unknown>;
}
export interface AgentStreamEventMeta {
    code?: string;
    message?: string;
    [key: string]: unknown;
}
export interface AgentStreamEvent {
    type?: string;
    step?: number;
    delta?: string;
    reply?: string;
    raw?: string;
    tool_call?: AgentToolCallPayload;
    tool_result?: AgentToolResultPayload;
    meta?: AgentStreamEventMeta;
}
export interface DeleteResult {
    deleted: boolean;
}
