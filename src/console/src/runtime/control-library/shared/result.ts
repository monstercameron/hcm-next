export type ControlConfigErrorCode =
  | "CONTROL_CONFIG_INVALID_RECORD"
  | "CONTROL_CONFIG_INVALID_FIELD"
  | "CONTROL_CONFIG_INVALID_PATH"
  | "CONTROL_CONFIG_INVALID_RECORD_LIST";

export type ControlConfigError = {
  code: ControlConfigErrorCode;
  message: string;
  safeMessage: string;
  details?: Readonly<Record<string, unknown>>;
};

export type ControlResult<T> =
  | { ok: true; value: T }
  | { ok: false; error: ControlConfigError };

export const controlOk = <T>(value: T): ControlResult<T> => ({ ok: true, value });

export const controlErr = (
  code: ControlConfigErrorCode,
  message: string,
  details?: Readonly<Record<string, unknown>>,
): ControlResult<never> => ({
  ok: false,
  error:
    details === undefined
      ? {
          code,
          message,
          safeMessage: "The generated control configuration is invalid.",
        }
      : {
          code,
          message,
          safeMessage: "The generated control configuration is invalid.",
          details,
        },
});
