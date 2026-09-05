export type BindingNode = {
  id: string;
  accountCapacity: number;
  assignedAccountCount: number;
};

export type EgressBindingAssignment = {
  nodeID: string;
  accountIDs: string[];
};

export type EgressBindingDefaults = {
  defaultPerNode: number;
  maxPerNode: number;
};

export type EgressBindingScope = "grok_build" | "grok_web" | "grok_console" | "grok_web_asset" | "grok_console_asset";
export type EgressBindingProvider = "grok_build" | "grok_web" | "grok_console";

export type EgressBindingPlan = {
  assignments: EgressBindingAssignment[];
  unassignedAccountIDs: string[];
  perNodeLimit: number;
};

function nonNegativeInteger(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.floor(value)) : 0;
}

export function supportsEgressBindingScope(scope: EgressBindingScope, provider: EgressBindingProvider): boolean {
  return scope === provider;
}

export function getEgressBindingCapacity(node: BindingNode, selectedAccountCount: number): number {
  const selectedCount = nonNegativeInteger(selectedAccountCount);
  const configuredCapacity = nonNegativeInteger(node.accountCapacity);
  if (configuredCapacity === 0) return selectedCount;
  return Math.max(0, configuredCapacity - nonNegativeInteger(node.assignedAccountCount));
}

export function getEgressBindingDefaults(selectedAccountCount: number, nodes: BindingNode[]): EgressBindingDefaults {
  const accountCount = nonNegativeInteger(selectedAccountCount);
  if (accountCount === 0 || nodes.length === 0) return { defaultPerNode: 0, maxPerNode: 0 };
  const totalCapacity = nodes.reduce((total, node) => total + getEgressBindingCapacity(node, accountCount), 0);
  const maxPerNode = Math.floor(totalCapacity / nodes.length);
  const defaultPerNode = Math.min(maxPerNode, Math.max(1, Math.ceil(accountCount / nodes.length)));
  return { defaultPerNode, maxPerNode };
}

export function normalizeEgressBindingLimit(value: string, maxPerNode: number): string {
  const raw = value.trim();
  const maximum = nonNegativeInteger(maxPerNode);
  if (raw === "" || maximum === 0) return "";
  const parsed = Number(raw);
  if (!Number.isInteger(parsed) || parsed <= 0) return "";
  return String(Math.min(parsed, maximum));
}

export function planEgressBinding(accountIDs: string[], nodes: BindingNode[], requestedPerNode: number): EgressBindingPlan {
  const defaults = getEgressBindingDefaults(accountIDs.length, nodes);
  const requested = nonNegativeInteger(requestedPerNode);
  const perNodeLimit = defaults.maxPerNode === 0
    ? 0
    : Math.min(defaults.maxPerNode, requested > 0 ? requested : defaults.defaultPerNode);
  const remaining = nodes.map((node) => getEgressBindingCapacity(node, accountIDs.length));
  const assignments = nodes.map((node) => ({ nodeID: node.id, accountIDs: [] as string[] }));
  const unassignedAccountIDs: string[] = [];
  if (nodes.length === 0 || perNodeLimit === 0) {
    return { assignments: [], unassignedAccountIDs: [...accountIDs], perNodeLimit };
  }

  let cursor = 0;
  for (const accountID of accountIDs) {
    let placed = false;
    for (let scanned = 0; scanned < nodes.length; scanned += 1) {
      const index = cursor;
      cursor = (cursor + 1) % nodes.length;
      if (remaining[index] <= 0 || assignments[index].accountIDs.length >= perNodeLimit) continue;
      assignments[index].accountIDs.push(accountID);
      remaining[index] -= 1;
      placed = true;
      break;
    }
    if (!placed) unassignedAccountIDs.push(accountID);
  }

  return {
    assignments: assignments.filter((assignment) => assignment.accountIDs.length > 0),
    unassignedAccountIDs,
    perNodeLimit,
  };
}
