package appliance

import (
	"fmt"
	"testing"
	"time"

	stewardv1alpha1 "github.com/caelis-labs/memory/api/memory/steward/v1alpha1"
	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

// TestRealisticChineseMixedCorpusAcrossRounds supplements marker-based release
// fixtures with reviewable Chinese/mixed-language product facts accumulated
// across restarts. It tests Memory application semantics, not model quality.
func TestRealisticChineseMixedCorpusAcrossRounds(t *testing.T) {
	type seed struct {
		query     string
		receipt   string
		canonical string
	}
	seeds := []seed{
		{query: "commit push", receipt: "发布约束：本地 commit 不代表允许 push。"},
		{query: "SQLite WAL", receipt: "Memory 使用 SQLite WAL，并在 accepted=true 前完成 durable commit。"},
		{query: "zero-call Replay", receipt: "旧 Session 的 zero-call Replay 必须逐字复用已持久化 ToolResult。"},
		{query: "memoryd sidecar", receipt: "Caelis 通过固定摘要启动或附着 memoryd sidecar。"},
		{query: "read-your-writes", receipt: "记忆写入后必须满足 read-your-writes，包括进程重启之后。"},
		{query: "OutputAudience", receipt: "OutputAudience 是 Caelis 运行时输出边界，不是 Memory 的分类标签。"},
		{query: "MemorySpace", receipt: "MemorySpace 决定存储和访问隔离，MemoryIdentity 只表示认知连续性。"},
		{query: "unknown outcome", receipt: "Remember 遇到 unknown outcome 时只能用相同 idempotency key 重试。"},
		{query: "private shared", receipt: "private 与 shared 候选必须先按 Space 授权，再进入合并排序。"},
		{query: "Go SDK", receipt: "Agent Host 只依赖版本化 Go SDK，不能访问 appliance internal 包。"},
		{query: "Windows named pipe", receipt: "Windows 本地传输采用 owner-restricted named pipe。"},
		{query: "Darwin Linux", receipt: "Darwin 和 Linux 使用 owner-only Unix Domain Socket。"},
		{query: "external review", receipt: "正式 GA 之前必须经过 external review 暂停点。"},
		{query: "memoryctl operator", receipt: "memoryctl 是 operator recovery artifact，不是 Agent 记忆入口。"},
		{query: "Plugin post-GA", receipt: "Plugin 是 post-GA 生态集成面，复用稳定 SDK 契约。"},
		{query: "Generator callback", receipt: "raw-memory-016", canonical: "下游通过 Generator callback 注入既有模型能力。"},
		{query: "claim apply fail", receipt: "raw-memory-017", canonical: "外部 Worker 只使用 claim apply fail 三类操作。"},
		{query: "lease proposal", receipt: "raw-memory-018", canonical: "lease 与 proposal 分离，模型永远看不到租约。"},
		{query: "same-Space Evidence", receipt: "raw-memory-019", canonical: "每个派生 Revision 必须引用 same-Space Evidence。"},
		{query: "baseline Recall", receipt: "raw-memory-020", canonical: "没有 Worker 时 baseline Recall 仍然可用。"},
		{query: "provider configuration", receipt: "raw-memory-021", canonical: "provider configuration 完全属于下游，不进入 memoryd。"},
		{query: "prompt-policy profile", receipt: "raw-memory-022", canonical: "appliance 仅持久化 prompt-policy profile 与输入输出预算。"},
		{query: "semantic mutation", receipt: "raw-memory-023", canonical: "semantic mutation 只能由 appliance 校验后原子提交。"},
		{query: "private leakage", receipt: "raw-memory-024", canonical: "GA 的 private leakage 允许值为零。"},
	}

	dataDir := t.TempDir()
	store, auth := newGoldenStore(t, dataDir, time.Now)
	t.Cleanup(func() { _ = store.Close() })
	type expected struct {
		seed
		receiptID v1alpha1.ReceiptID
		recordID  stewardv1alpha1.RecordID
	}
	all := make([]expected, 0, len(seeds))
	const rounds = 3
	for round := range rounds {
		start := round * len(seeds) / rounds
		end := (round + 1) * len(seeds) / rounds
		for index, item := range seeds[start:end] {
			absolute := start + index
			remembered, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
				Text: item.receipt, IdempotencyKey: fmt.Sprintf("realistic-corpus-%02d", absolute),
			})
			if err != nil {
				t.Fatal(err)
			}
			result := expected{seed: item, receiptID: remembered.ReceiptID}
			if item.canonical != "" {
				lease := leaseStewardReceipt(t, store, remembered.ReceiptID,
					stewardv1alpha1.JobID(fmt.Sprintf("job-realistic-corpus-%02d", absolute)))
				applied, err := store.ApplyStewardProposal(t.Context(), lease, stewardv1alpha1.Proposal{
					Operation: stewardv1alpha1.OperationAdd, Kind: "claim", Text: item.canonical,
					EvidenceRefs: []v1alpha1.ReceiptID{remembered.ReceiptID},
				})
				if err != nil {
					t.Fatal(err)
				}
				result.recordID = applied.RecordID
			}
			all = append(all, result)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		var err error
		store, err = Open(t.Context(), Options{DataDir: dataDir})
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, item := range all {
		response, err := store.Recall(t.Context(), auth, testRecall(item.query, ""))
		if err != nil {
			t.Fatalf("Recall %q: %v", item.query, err)
		}
		wantText := item.receipt
		if item.canonical != "" {
			wantText = item.canonical
		}
		if response.Degraded || len(response.Fragments) == 0 || response.Fragments[0].Text != wantText {
			t.Fatalf("Recall %q = %+v, want first text %q", item.query, response, wantText)
		}
		fragment := response.Fragments[0]
		if len(fragment.EvidenceRefs) != 1 || fragment.EvidenceRefs[0] != item.receiptID {
			t.Fatalf("Recall %q provenance = %+v", item.query, fragment)
		}
		if item.recordID != "" && (len(fragment.RecordRefs) != 1 || fragment.RecordRefs[0] != string(item.recordID)) {
			t.Fatalf("Recall %q Record provenance = %+v", item.query, fragment.RecordRefs)
		}
	}
}
