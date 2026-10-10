export function mergeVisibleBoundAccountSelection(
  currentIds: readonly string[],
  visibleIds: readonly string[],
  selectVisible: boolean,
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
