import type { TaskMessagePayload } from "@multica/core/types/events";
import { redactSecrets } from "./redact";

/** A unified timeline entry: tool calls, thinking, reasoning, text, and errors in chronological order. */
export interface TimelineItem {
  seq: number;
  type: "tool_use" | "tool_result" | "thinking" | "reasoning" | "text" | "error";
  tool?: string;
  content?: string;
  // text carries reasoning content for type=reasoning. Same underlying
  // value as content (server stores reasoning in content column but surfaces
  // it as text); kept distinct so the UI can render reasoning with its own
  // treatment. Undefined for other types.
  text?: string;
  input?: Record<string, unknown>;
  output?: string;
}

/** Build a chronologically ordered timeline from raw task messages. */
export function buildTimeline(msgs: TaskMessagePayload[]): TimelineItem[] {
  const items: TimelineItem[] = [];
  for (const msg of msgs) {
    items.push({
      seq: msg.seq,
      type: msg.type,
      tool: msg.tool,
      content: msg.content ? redactSecrets(msg.content) : msg.content,
      text: msg.text ? redactSecrets(msg.text) : msg.text,
      input: msg.input,
      output: msg.output ? redactSecrets(msg.output) : msg.output,
    });
  }
  return items.sort((a, b) => a.seq - b.seq);
}
