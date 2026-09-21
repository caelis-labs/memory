# v0.6.0 质量审查复现证据

对应 [质量审查与修复计划](../../memory-v0.6-quality-review-2026-09-21.md)。本目录只保存本轮证据，没有修改产品行为。

- [v0.5.2 数据生成测试](reproduction-v052_test.go.txt)：在真实旧源码上创建内置策略、绑定及待处理 Receipt。
- [v0.6.0 复现测试](reproduction-current_test.go.txt)：策略冲突与新版本诊断对照、传递治理回执终态回归、同主体读取成本诊断。
- [观测汇总](observations.json)：精确源码、环境、命令、退出码、实际观察值及证据文件哈希；这是工具输出的结构化汇总，不是完整终端日志。
- [本轮 Facts 评估原始报告](facts-eval.json)：从实际执行门禁生成的文件复制，未改变其结果和资格限制。

所有数据均为隔离合成数据，没有模型调用。复现代码只使用现有测试辅助函数设置基础授权，行为经过真实 Store 的摄入、任务、治理和读取路径；不是 public Runtime/Runner 消费测试的替代品。

## 重现步骤

需要可用的 Go 工具链、依赖缓存及本地准确 release Git 对象。从仓库根目录执行；每次从新的临时目录开始，因为策略诊断对照会在测试库注册版本 2。

```sh
cd /Users/xueyongzhi/WorkDir/caelis-labs/memory
review_dir=$(mktemp -d /tmp/memory-v060-review.XXXXXX)
mkdir "$review_dir/old" "$review_dir/current" "$review_dir/data"
git archive 51693ff135be8c4149c15117980290aaad6d90da | tar -x -C "$review_dir/old"
git archive f17b0293597dcdf9fad4fcba9ea19e20d1d91674 | tar -x -C "$review_dir/current"
cp docs/evidence/memory-v0.6-review-2026-09-21/reproduction-v052_test.go.txt "$review_dir/old/internal/appliance/review_seed_test.go"
cp docs/evidence/memory-v0.6-review-2026-09-21/reproduction-current_test.go.txt "$review_dir/current/internal/appliance/review_reproduction_test.go"

cd "$review_dir/old"
GOWORK=off REVIEW_DB="$review_dir/data" go test ./internal/appliance -run '^TestReviewSeedReleasedBuiltin$' -count=1 -v
cd "$review_dir/current"
GOWORK=off REVIEW_DB="$review_dir/data" go test ./internal/appliance -run '^TestReviewBuiltinUpgradeCollision$' -count=1 -v
GOWORK=off go test ./internal/appliance -run '^TestReviewTransitiveGovernanceStatus$' -count=1 -v
GOWORK=off go test ./internal/appliance -run '^TestReviewSameSubjectReadCost$' -count=1 -v
```

原始 v0.6.0 上，策略测试的 PASS 表示成功重现了预期的三次冲突，并完成“分配新版本”的诊断对照，不能解释为版本升级通过。正式修复后的回归应改为断言注册当前 BuiltInProfile 成功，不再手工递增版本。

治理测试预期 FAIL，分别在 delete/correct 的立即读取和重启后读到 processing/failed 不一致。性能测试只记录诊断值，不设置未经批准的 release SLO。若 shell 开启了遇错退出，应将最后两条命令分别执行，以免治理用例的预期失败阻止性能诊断。

本轮实际临时目录为 `/tmp/memory-v060-review-pmbp08cj`；长期复现应按以上步骤重建，不能依赖临时目录一直存在。
