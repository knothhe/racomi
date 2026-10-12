type PlanChanges = {
  add?: string[]; update?: string[]; remove?: string[]; conflicts?: string[];
  overwrite?: string[]; blockingConflicts?: string[];
};

export function applicationPlanDisposition(plan: PlanChanges): "ready" | "overwrite" | "blocked" {
  if (plan.blockingConflicts?.length) return "blocked";
  if (!plan.conflicts?.length) return "ready";
  // Older plans do not distinguish conflicts that are safe to override.
  return Array.isArray(plan.blockingConflicts) && !!plan.overwrite?.length ? "overwrite" : "blocked";
}

export function hasApplicationChanges(plan: PlanChanges): boolean {
  return !!(plan.add?.length || plan.update?.length || plan.remove?.length || plan.overwrite?.length);
}
