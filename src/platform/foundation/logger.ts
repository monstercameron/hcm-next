import type { RequestContext } from "./context.js";

export type LogLevel = "info" | "warn" | "error";

export type LogEvent = {
  level: LogLevel;
  message: string;
  context?: Partial<RequestContext>;
  details?: Record<string, unknown>;
};

/**
 * Minimal structured logger for V0.
 *
 * This intentionally keeps console usage centralized behind one boundary.
 */
export function writeLog(event: LogEvent): void {
  const serializedEvent = JSON.stringify({
    level: event.level,
    message: event.message,
    context: event.context ?? {},
    details: event.details ?? {},
    timestamp: new Date().toISOString(),
  });

  if (event.level === "error") {
    process.stderr.write(`${serializedEvent}\n`);
    return;
  }

  process.stdout.write(`${serializedEvent}\n`);
}
