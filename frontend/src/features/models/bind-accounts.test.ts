import test from "node:test";
import assert from "node:assert/strict";

import {
  MAX_BOUND_ACCOUNTS,
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

test("keeps existing ids then fills up to the bind cap", () => {
  const current = Array.from({ length: 998 }, (_, index) => `keep-${index}`);
  const visible = ["keep-0", "new-a", "new-b", "new-c", "new-d"];
  const next = mergeVisibleBoundAccountSelection(current, visible, true);
  assert.equal(next.length, MAX_BOUND_ACCOUNTS);
  assert.deepEqual(next.slice(0, 998), current);
  assert.deepEqual(next.slice(998), ["new-a", "new-b"]);
});

test("does not drop existing ids when the bind cap is already full", () => {
  const current = Array.from({ length: MAX_BOUND_ACCOUNTS }, (_, index) => `keep-${index}`);
  const next = mergeVisibleBoundAccountSelection(current, ["extra"], true);
  assert.equal(next.length, MAX_BOUND_ACCOUNTS);
  assert.equal(next.includes("extra"), false);
  assert.deepEqual(next, current);
  assert.notEqual(next, current);
});

test("does not mutate the current ids array", () => {
  const current = Object.freeze(["a"]);
  const next = mergeVisibleBoundAccountSelection(current, ["a", "b"], true);
  assert.notEqual(next, current);
  assert.deepEqual([...current], ["a"]);
  assert.deepEqual(next, ["a", "b"]);
});
