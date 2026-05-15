const idCounters = new Map<string, number>();

/** Creates stable, readable IDs for V0 workflow objects. */
export function createReadableId(prefix: string): string {
  const nextValue = (idCounters.get(prefix) ?? 0) + 1;
  idCounters.set(prefix, nextValue);
  return `${prefix}_${Date.now().toString(36)}_${nextValue.toString(36)}`;
}
