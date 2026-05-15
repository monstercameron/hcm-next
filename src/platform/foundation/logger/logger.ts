import type { RequestContext, WorkflowContext } from "../context";
import type { JsonRecord } from "../domain";

export type LogLevel = "debug" | "info" | "warn" | "error";

export type LogContext = Partial<RequestContext> &
  Partial<Pick<WorkflowContext, "workflowInstanceId" | "changeRequestId">> & {
    service?: "api" | "executor";
  };

export type StructuredLogger = {
  debug: (message: string, details?: JsonRecord) => void;
  info: (message: string, details?: JsonRecord) => void;
  warn: (message: string, details?: JsonRecord) => void;
  error: (message: string, details?: JsonRecord) => void;
};

function writeStructuredLog(
  level: LogLevel,
  baseContext: LogContext,
  message: string,
  details?: JsonRecord,
): void {
  const logEntry = {
    timestamp: new Date().toISOString(),
    level,
    message,
    ...baseContext,
    details: details ?? {},
  };
  const serializedLogEntry = `${JSON.stringify(logEntry)}\n`;
  const stream =
    level === "error" || level === "warn" ? process.stderr : process.stdout;

  stream.write(serializedLogEntry);
}

/**
 * Creates a minimal structured logger that carries request and workflow context.
 */
export function createStructuredLogger(baseContext: LogContext = {}): StructuredLogger {
  return {
    debug(message, details) {
      writeStructuredLog("debug", baseContext, message, details);
    },
    info(message, details) {
      writeStructuredLog("info", baseContext, message, details);
    },
    warn(message, details) {
      writeStructuredLog("warn", baseContext, message, details);
    },
    error(message, details) {
      writeStructuredLog("error", baseContext, message, details);
    },
  };
}
