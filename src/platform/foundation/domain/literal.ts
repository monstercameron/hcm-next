export type ValueOf<TValue extends Record<string, unknown>> = TValue[keyof TValue];
