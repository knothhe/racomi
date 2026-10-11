// Bulk controls only change the visible IDs; hidden selections remain intact.
export function changeSelection(selection: ReadonlySet<string>, visibleIDs: string[], action: "toggle-all" | "invert"): Set<string> {
  const next = new Set(selection);
  const removeAll = action === "toggle-all" && visibleIDs.every((id) => selection.has(id));
  for (const id of visibleIDs) {
    if (removeAll || (action === "invert" && selection.has(id))) next.delete(id);
    else next.add(id);
  }
  return next;
}
