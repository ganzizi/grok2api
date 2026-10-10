import test from "node:test";
import assert from "node:assert/strict";

import {
  mergeVisibleBoundAccountSelection,
  visibleBoundAccountsFullySelected,
} from "./bind-accounts.ts";

test("selects visible accounts and keeps hidden selections", () => {
  const next = mergeVisibleBoundAccountSelection(["hidden", "a"], ["a", "b", "c"], true);
  assert.deepEqual(next, ["hidden", "a", "b", "c"]);
});

test("clears only visible accounts and keeps hidden selections", () => {
  const next = mergeVisibleBoundAccountSelection(["hidden", "a", "b"], ["a", "b"], false);
  assert.deepEqual(next, ["hidden"]);
});

test("does not treat an empty visible list as fully selected", () => {
  assert.equal(visibleBoundAccountsFullySelected(["a"], []), false);
});

test("reports fully selected only when every visible id is already chosen", () => {
  assert.equal(visibleBoundAccountsFullySelected(["a", "hidden"], ["a"]), true);
  assert.equal(visibleBoundAccountsFullySelected(["hidden"], ["a"]), false);
});

test("selects every visible account past the old 1000 row cap", () => {
  const visible = Array.from({ length: 1500 }, (_, index) => `id-${index}`);
  const next = mergeVisibleBoundAccountSelection([], visible, true);
  assert.equal(next.length, 1500);
  assert.equal(next[1499], "id-1499");
});

test("does not mutate the current ids array", () => {
  const current = Object.freeze(["a"]);
  const next = mergeVisibleBoundAccountSelection(current, ["a", "b"], true);
  assert.notEqual(next, current);
  assert.deepEqual([...current], ["a"]);
  assert.deepEqual(next, ["a", "b"]);
});
