# 账号多节点代理绑定 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在账号管理页支持选择多个兼容代理节点，计算每节点绑定数量的默认值和最大值，允许管理员编辑后按均衡规则绑定选中账号。

**Architecture:** 保留现有后端单节点绑定 API 和账号单节点数据模型。新增一个前端纯函数模块负责容量、默认值、最大值和轮询分配；账号页负责选择状态、展示和依次调用现有 API。三类 Provider 继续使用现有作用域过滤。

**Tech Stack:** React 19、TypeScript、TanStack Query、现有 Checkbox/Input/Dialog、Node test runner、Go 测试。

---

## 文件边界

- Create: `frontend/src/features/accounts/egress-binding.ts`，纯分配算法和数据类型。
- Test: `frontend/src/features/accounts/egress-binding.test.ts`，容量、默认值和分配边界测试。
- Modify: `frontend/src/features/accounts/accounts-page.tsx`，多节点选择、数量输入和提交结果。
- Modify: `frontend/src/shared/i18n/index.ts`，中英文界面提示。
- No backend source change expected; reuse `POST /api/admin/v1/egress-nodes/:nodeId/accounts`.

## Task 1: Add and test the pure binding planner

**Files:**
- Create: `frontend/src/features/accounts/egress-binding.ts`
- Test: `frontend/src/features/accounts/egress-binding.test.ts`

- [ ] **Step 1: Write the failing tests**

Test remaining capacity, average maximum, zero-capacity unlimited nodes, round-robin distribution, node capacity limits, and invalid requested limits. The test must import `getEgressBindingCapacity`, `getEgressBindingDefaults`, and `planEgressBinding` before those exports exist.

```ts
import test from "node:test";
import assert from "node:assert/strict";
import { getEgressBindingCapacity, getEgressBindingDefaults, planEgressBinding } from "./egress-binding";

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
```

- [ ] **Step 2: Run the focused test and verify the expected missing-module failure**

Run from `frontend`: `pnpm test -- --test-name-pattern="binding"`.
Expected: compilation/test failure because `egress-binding.ts` is not present.

- [ ] **Step 3: Implement the minimal planner**

Use these contracts:

```ts
export type BindingNode = { id: string; accountCapacity: number; assignedAccountCount: number };
export type EgressBindingAssignment = { nodeID: string; accountIDs: string[] };
export type EgressBindingDefaults = { defaultPerNode: number; maxPerNode: number };
export type EgressBindingPlan = { assignments: EgressBindingAssignment[]; unassignedAccountIDs: string[]; perNodeLimit: number };
```

For finite capacity use `max(0, accountCapacity - assignedAccountCount)`. For capacity `0`, use the selected account count as a finite planning cap. Set `maxPerNode` to `floor(totalRemaining / nodeCount)` and `defaultPerNode` to the smaller of that value and `ceil(accountCount / nodeCount)`. Clamp the requested limit to `0..maxPerNode`; distribute with a round-robin cursor while respecting both the per-node limit and each node's remaining capacity.

- [ ] **Step 4: Run the focused tests and verify they pass**

Run: `pnpm test -- --test-name-pattern="binding"`.
Expected: all binding planner tests pass.

- [ ] **Step 5: Commit the planner**

```sh
git add frontend/src/features/accounts/egress-binding.ts frontend/src/features/accounts/egress-binding.test.ts
git commit -m "feat: add multi-node account binding planner"
```

## Task 2: Integrate the planner into the account page

**File:** `frontend/src/features/accounts/accounts-page.tsx`.

- [ ] **Step 1: Replace single-node state**

Replace `egressNodeID` with `egressNodeIDs: Set<string>` and `egressPerNodeLimit: string`. Keep `bindEgressMutation` and `unbindEgressMutation` separate.

- [ ] **Step 2: Derive compatible nodes and limits**

Reuse `bindableEgressNodes`, which already filters enabled/configured nodes through `scopeSupportsAccountProvider`. Map selected IDs back to those DTOs, call the planner defaults with `selected.size`, and use the calculated maximum for the input `max` attribute. Reset the input to the new default when a node is selected or removed and the old value is empty/out of range.

- [ ] **Step 3: Add multi-select controls**

Replace the existing Radix `Select` with a bounded scrollable list of Checkbox rows. Each row shows node name, assigned count, and finite capacity or the existing unlimited label. Display selected node count, automatic default, and maximum.

- [ ] **Step 4: Submit grouped assignments through the existing API**

Snapshot selected account IDs, provider, selected node DTOs, and the normalized per-node limit in `mutate`. Call `planEgressBinding`, then invoke `assignEgressAccounts` sequentially for each non-empty assignment. Stop on the first API error, retain the count already submitted in a typed error, invalidate account/egress queries, clear selection, close the dialog, and show a partial-result warning. Do not continue after an uncertain failed request.

- [ ] **Step 5: Keep unbind behavior unchanged**

The unbind action continues to call `unassignEgressAccounts(provider, [...selected])`; its dialog text and API payload must remain unchanged.

- [ ] **Step 6: Run frontend checks**

Run `pnpm lint`, `pnpm test`, and `pnpm build` from `frontend`; all must exit 0.

- [ ] **Step 7: Commit the page integration**

```sh
git add frontend/src/features/accounts/accounts-page.tsx
git commit -m "feat: support multi-node account proxy binding"
```

## Task 3: Add localized messages

**File:** `frontend/src/shared/i18n/index.ts`.

- [ ] **Step 1: Add Chinese and English keys**

Add keys for node multi-selection, per-node count, automatic default, maximum, no capacity, full success, partial success, assigned count, and unassigned count. Keep existing single-node and unbind keys for compatibility.

- [ ] **Step 2: Run frontend checks and commit**

Run `pnpm lint`, `pnpm test`, and `pnpm build`, then commit with `git commit -m "feat: localize multi-node binding controls"`.

## Task 4: Verify and deploy

- [ ] **Step 1: Run backend checks without changing backend code**

From `backend`, run `go test ./...`, `go vet ./...`, and `go build ./cmd/grok2api`.

- [ ] **Step 2: Build the Docker image**

From the repository root run `docker build --tag grok2api-multi-node-binding-check:local --file Dockerfile .`.

- [ ] **Step 3: Push only `main`**

Confirm `git status --short` is empty, then run `git push origin main`; do not create or force-push a feature branch.

- [ ] **Step 4: Deploy only `ganzizi-grok2api-15353`**

On the server, fast-forward the existing checkout to `origin/main`, preserve the existing data volumes, rebuild only the named Compose project, wait for `/healthz` to return HTTP 200, and verify the quality guard plus FlareSolverr remain running.

- [ ] **Step 5: Verify Build, Web, and Console**

Use a temporary client Key to load all three account/node views and make one real streaming request for each Provider. Confirm the temporary Key is deleted afterward.

- [ ] **Step 6: Verify multi-node binding without altering production data**

Use disposable or explicitly controlled test accounts and at least two compatible nodes per Provider. Capture their original bindings, verify the calculated maximum and edited limit, bind, read back each account to confirm exactly one selected node and per-node counts, then restore the captured bindings.

- [ ] **Step 7: Final state check**

Confirm local `main` and `origin/main` match and are clean. Confirm the server source and deployment marker match, the main service is healthy, the quality guard is running, and unrelated service containers retain their prior state.
