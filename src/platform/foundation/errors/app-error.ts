import type { JsonRecord } from "../domain";
import type { ErrorCode } from "./error-codes";

export type AppError = {
  code: ErrorCode;
  message: string;
  safeMessage: string;
  details?: JsonRecord;
  cause?: unknown;
};
