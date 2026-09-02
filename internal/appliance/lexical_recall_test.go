package appliance

import (
	"testing"
	"time"

	v1alpha1 "github.com/caelis-labs/memory/api/memory/v1alpha1"
)

func TestRecallUnspacedChineseQueries(t *testing.T) {
	store, auth := newGoldenStore(t, t.TempDir(), time.Now)
	defer store.Close()
	memories := []string{
		"张三的个人信息包括后端工程师岗位、Go 技能和 2025 年入职时间。",
		"项目技术栈采用 React、TypeScript、PostgreSQL，并部署在 Vercel。",
		"团队会议安排在周三，会议内容是代码评审和 Pull Requests 流程。",
	}
	for index, text := range memories {
		if _, err := store.Remember(t.Context(), auth, v1alpha1.RememberRequest{
			Text: text, IdempotencyKey: "chinese-lexical-" + string(rune('a'+index)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		query string
		want  string
	}{
		{query: "项目技术栈", want: memories[1]},
		{query: "团队会议安排", want: memories[2]},
		{query: "个人信息", want: memories[0]},
		{query: "PostgreSQL Vercel", want: memories[1]},
		{query: "周三代码评审", want: memories[2]},
	}
	for _, test := range tests {
		response, err := store.Recall(t.Context(), auth, testRecall(test.query, ""))
		if err != nil {
			t.Fatalf("Recall(%q): %v", test.query, err)
		}
		if len(response.Fragments) == 0 || response.Fragments[0].Text != test.want {
			t.Fatalf("Recall(%q) = %+v, want first %q", test.query, response.Fragments, test.want)
		}
	}
}
