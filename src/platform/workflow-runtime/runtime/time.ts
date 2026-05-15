/** Returns an ISO timestamp for persisted runtime records and ledger events. */
export function nowIso(): string {
  return new Date().toISOString();
}
