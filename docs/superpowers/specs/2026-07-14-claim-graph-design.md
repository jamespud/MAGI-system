# Claim Graph 真图设计（§11）

> 设计日期：2026-07-14
> 关联：闭合 magi-design.md §11（Claim Graph）、§25（"最有价值数据结构"）
> 前置：当前 `domain/claim/graph.go` 是薄包装（直接读 Claim.Supports/Contradicts 字段），无边对象、无双向索引、无遍历。

## 目标

将 Claim Graph 升级为真正的图数据结构：内部边对象/双向邻接索引、反向查询（EV->claims）、BFS 遍历（传递冲突组件）、矛盾对去重。闭合 §11 与 §25 的图结构缺口。

## 范围

仅 `domain/claim/graph.go` 升级 + 新建 `domain/claim/graph_test.go`。ConflictFinder 不改动（继续用 `Conflicts()`，现去重）。

## 设计

### 内部结构（NewGraph 时构建）

```go
type Graph struct {
	claims           map[string]*entity.Claim
	evidenceToClaims map[string][]string        // EV-ID -> 支持它的 claim IDs（反向索引）
	contradictions   map[string]map[string]bool // 双向矛盾邻接（A<->B）
}
```

NewGraph 遍历 claims：填充 `claims`、`evidenceToClaims`（从 Supports）、`contradictions`（从 Contradicts，addEdge 双向）。

### 方法

- `Supports(claimID) []string`：不变（读 Claim.Supports）。
- `Contradicts(claimID) []string`：改用双向索引，返回所有矛盾方（双向，含被该 claim 矛盾的与矛盾该 claim 的）。
- `ClaimsForEvidence(evID string) []string`：[新] 反向查询支持某 EV 的 claims。
- `ConflictComponent(claimID string) []string`：[新] BFS 遍历 contradictions 邻接，返回与 claimID 传递性矛盾的所有 claims（冲突组件）。
- `Conflicts() []ClaimConflict`：改用双向索引去重输出唯一对（当前 A.Contradicts=[B] 且 B.Contradicts=[A] 会输出 (A,B)+(B,A) 重复；改为按排序 key 去重）。

### 去重逻辑

`Conflicts()` 遍历 contradictions 邻接，用 `seen map[string]bool`（key = `min(a,b)+"|"+max(a,b)`）去重，每个矛盾对只输出一次。

### 兼容性

`NewGraph`、`Supports`、`Conflicts` 签名不变，ConflictFinder.NilConflictFinder 继续工作。`Contradicts` 语义增强（双向），其现有调用方（debate 等）需确认无破坏。

## 测试

新建 `domain/claim/graph_test.go`（`package claim_test`）：
- `TestGraph_ClaimsForEvidence`：claims 支持 EV-001 -> ClaimsForEvidence("EV-001") 返回这些 claims。
- `TestGraph_ContradictsBidirectional`：A.Contradicts=[B] -> Contradicts("A") 含 B，Contradicts("B") 含 A。
- `TestGraph_ConflictComponent`：A↔B↔C 链 -> ConflictComponent("A") 返回 {A,B,C}。
- `TestGraph_ConflictsDedup`：A↔B 互相断言 -> Conflicts() 只输出 1 对。

## 文件清单

- `domain/claim/graph.go`：升级 Graph 结构 + NewGraph + 方法。
- `domain/claim/graph_test.go`（新建）：4 个测试。
