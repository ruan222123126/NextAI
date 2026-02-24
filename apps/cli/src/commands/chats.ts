import { Command } from "commander";
import { appendQuery, fillPath } from "@nextai/sdk-ts";
import { ApiClient, ApiClientError } from "../client/api-client.js";
import { DEFAULT_CHANNEL } from "../constants.js";
import { printResult } from "../io/output.js";
import { t } from "../i18n.js";

export function registerChatsCommand(program: Command, client: ApiClient): void {
  const chats = program.command("chats").description(t("cli.command.chats"));

  chats
    .command("list")
    .option("--user-id <userId>")
    .option("--channel <channel>")
    .action(async (opts: { userId?: string; channel?: string }) => {
      printResult(await client.get(appendQuery("/chats", {
        user_id: opts.userId,
        channel: opts.channel,
      })));
    });

  chats
    .command("create")
    .requiredOption("--session-id <sid>")
    .requiredOption("--user-id <uid>")
    .option("--channel <channel>", DEFAULT_CHANNEL)
    .option("--name <name>", t("chats.default_name"))
    .action(async (opts: { sessionId: string; userId: string; channel: string; name: string }) => {
      const payload = {
        name: opts.name,
        session_id: opts.sessionId,
        user_id: opts.userId,
        channel: opts.channel,
        meta: {},
      };
      printResult(await client.post("/chats", payload));
    });

  chats
    .command("delete")
    .argument("<chatId>")
    .action(async (chatId: string) => {
      printResult(await client.delete(fillPath("/chats/{chat_id}", { chat_id: chatId })));
    });

  chats
    .command("get")
    .argument("<chatId>")
    .action(async (chatId: string) => {
      printResult(await client.get(fillPath("/chats/{chat_id}", { chat_id: chatId })));
    });

  chats
    .command("send")
    .requiredOption("--chat-session <sid>")
    .requiredOption("--user-id <uid>")
    .option("--channel <channel>", DEFAULT_CHANNEL)
    .requiredOption("--message <message>")
    .option("--stream")
    .action(async (opts: { chatSession: string; userId: string; channel: string; message: string; stream: boolean }) => {
      const payload = {
        input: [{ role: "user", type: "message", content: [{ type: "text", text: opts.message }] }],
        session_id: opts.chatSession,
        user_id: opts.userId,
        channel: opts.channel,
        stream: Boolean(opts.stream),
      };
      if (!opts.stream) {
        printResult(await client.post("/agent/process", payload));
        return;
      }
      const response = await client.openStream("/agent/process", {
        method: "POST",
        body: payload,
        accept: "text/event-stream,application/json",
      });
      if (!response.body) {
        throw new ApiClientError({
          status: 500,
          code: "stream_unsupported",
          message: "stream unsupported",
          payload: {},
        });
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        process.stdout.write(decoder.decode(value, { stream: true }));
      }
    });
}
