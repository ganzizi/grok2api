import test from "node:test";
import assert from "node:assert/strict";

import { getEgressBindingCapacity, getEgressBindingDefaults, planEgressBinding } from "./egress-binding.ts";

const nodes = [
  { id: "1", accountCapacity: 10, assignedAccountCount: 4 },
  { id: "2", accountCapacity: 8, assignedAccountCount: 2 },
];

test("calculates remaining capacity and average default", () => {
  assert.equal(getEgressBindingCapacity(nodes[0], 6), 6);
  assert.deepEqual(getEgressBindingDefaults(10, nodes), { defaultPerNode: 5, maxPerNode: 6 });
});

test("caps an unlimited node by the selected account count", () => {
  const node = { id: "1", accountCapacity: 0, assignedAccountCount: 999 };
  assert.equal(getEgressBindingCapacity(node, 12), 12);
  assert.deepEqual(getEgressBindingDefaults(12, [node]), { defaultPerNode: 12, maxPerNode: 12 });
});

test("distributes accounts round-robin within limits", () => {
  const result = planEgressBinding(["a", "b", "c", "d", "e"], nodes, 2);
  assert.deepEqual(result.assignments, [
    { nodeID: "1", accountIDs: ["a", "c"] },
    { nodeID: "2", accountIDs: ["b", "d"] },
  ]);
  assert.deepEqual(result.unassignedAccountIDs, ["e"]);
});

test("honors a smaller node capacity during distribution", () => {
  const result = planEgressBinding(["a", "b", "c", "d"], [
    { id: "1", accountCapacity: 2, assignedAccountCount: 1 },
    { id: "2", accountCapacity: 10, assignedAccountCount: 0 },
  ], 3);
  assert.deepEqual(result.assignments, [
    { nodeID: "1", accountIDs: ["a"] },
    { nodeID: "2", accountIDs: ["b", "c", "d"] },
  ]);
  assert.deepEqual(result.unassignedAccountIDs, []);
});

test("clamps an invalid requested limit to the calculated maximum", () => {
  const result = planEgressBinding(["a", "b"], nodes, 999);
  assert.equal(result.perNodeLimit, 6);
  assert.deepEqual(result.assignments, [
    { nodeID: "1", accountIDs: ["a"] },
    { nodeID: "2", accountIDs: ["b"] },
  ]);
  assert.deepEqual(result.unassignedAccountIDs, []);
});

test("returns no assignments when selected nodes have no remaining capacity", () => {
  const result = planEgressBinding(["a"], [{ id: "1", accountCapacity: 1, assignedAccountCount: 1 }], 1);
  assert.deepEqual(result.assignments, []);
  assert.deepEqual(result.unassignedAccountIDs, ["a"]);
});
