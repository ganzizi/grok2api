export const MAX_BOUND_ACCOUNTS = 1000;

export function mergeVisibleBoundAccountSelection(
  currentIds: readonly string[],
  visibleIds: readonly string[],
  selectVisible: boolean,
  max = MAX_BOUND_ACCOUNTS,
): string[] {
  if (!selectVisible) {
    const visible = new Set(visibleIds);
    return currentIds.filter((id) => !visible.has(id));
  }
  const selected = new Set(currentIds);
  const next = [...currentIds];
  for (const id of visibleIds) {
    if (selected.has(id)) {
      continue;
    }
    if (next.length >= max) {
      break;
    }
    selected.add(id);
    next.push(id);
  }
  return next;
}

export function visibleBoundAccountsFullySelected(
  currentIds: readonly string[],
  visibleIds: readonly string[],
): boolean {
  if (visibleIds.length === 0) {
    return false;
  }
  const selected = new Set(currentIds);
  return visibleIds.every((id) => selected.has(id));
}
