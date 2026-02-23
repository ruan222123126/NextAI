export type Tone = "neutral" | "info" | "error";

export interface LoggingContext {
  statusLine: HTMLElement;
  rawRequestLabel: string;
  rawResponseLabel: string;
  resolveComposerStatus: () => {
    statusLocal: string;
    statusFullAccess: string;
  };
}

export interface Logger {
  setStatus(message: string, tone?: Tone): void;
  logComposerStatusToConsole(): void;
  logAgentRawRequest(raw: string): void;
  logAgentRawResponse(raw: string): void;
}

export function createLogger(context: LoggingContext): Logger {
  function setStatus(message: string, tone: Tone = "neutral"): void {
    context.statusLine.textContent = message;
    context.statusLine.classList.remove("error", "info");
    if (tone === "error" || tone === "info") {
      context.statusLine.classList.add(tone);
    }
    const payload = {
      tone,
      message,
      at: new Date().toISOString(),
    };
    if (tone === "error") {
      console.error("[NextAI][status]", payload);
      return;
    }
    console.log("[NextAI][status]", payload);
  }

  function logComposerStatusToConsole(): void {
    console.log("[NextAI][chat-composer-status]", context.resolveComposerStatus());
  }

  function logAgentRawRequest(raw: string): void {
    console.log(context.rawRequestLabel, {
      at: new Date().toISOString(),
      raw,
    });
  }

  function logAgentRawResponse(raw: string): void {
    console.log(context.rawResponseLabel, {
      at: new Date().toISOString(),
      raw,
    });
  }

  return {
    setStatus,
    logComposerStatusToConsole,
    logAgentRawRequest,
    logAgentRawResponse,
  };
}
