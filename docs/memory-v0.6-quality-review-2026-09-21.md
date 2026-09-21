# Memory v0.6.0 质量审查与修复计划

本文保留修复前的审查快照。后续实施与验证见 [v0.6.1 发布说明](memory-v0.6.1-release.md)。

审查日期：2026-09-21。结论：基础内核的工程约束和现有测试较完整，但 v0.6.0 存在一个应优先修复的旧库升级回归，以及一个已复现的治理状态一致性缺陷。建议发布新的补丁版本 v0.6.1；当前版本不宜作为启用 Steward 的既有数据库的常规升级目标。

## 1. 审查范围与版本身份

- 当前仓库：`/Users/xueyongzhi/WorkDir/caelis-labs/memory`。
- 本地 HEAD、v0.6.0 标签、远端发布标签均为 `f17b0293597dcdf9fad4fcba9ea19e20d1d91674`。
- 比较基线：v0.5.2，`51693ff135be8c4149c15117980290aaad6d90da`。
- 已实时读取 [issue #5](https://github.com/caelis-labs/memory/issues/5) 的正文、状态与评论；读取时为 OPEN、无评论，最后更新于 `2026-09-21T12:55:33Z`。
- 已核验 [v0.6.0 发布记录](https://github.com/caelis-labs/memory/releases/tag/v0.6.0)：正式发布，非 draft/prerelease。
- 检查范围包括 API/SDK 分层、可信证据摄入、事实生命周期与读取、Steward 策略与任务、授权分区、治理清理、迁移/恢复、测试和发布证据。
- 新增复现仅在从准确标签导出的临时源码副本和合成数据库中执行。未修改运行时、SDK、正式测试或发布配置，未提交、推送或操作真实数据。

这是一轮基于源码、现有门禁及定向复现的审查，不构成对所有执行路径的形式化证明。

## 2. 确定问题

### F1 · P1：内置 Steward 策略内容变化但版本未递增，旧库无法注册当前策略

位置：`/Users/xueyongzhi/WorkDir/caelis-labs/memory/sdk/go/memory/stewardworker/policy.go:14`、`:61`；冲突校验在 `/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/steward_profile.go:30`。

v0.5.2 与 v0.6.0 都返回 `memory-default@1`，后者改变了 `SystemPrompt`。数据库迁移保留旧版本后，`PutStewardProfile` 正确地拒绝同一个不可变键对应不同内容。该冲突不是重试可以消除的临时错误。

本轮用准确 v0.5.2 源码创建数据库，写入真实 `BuiltInProfile()`、绑定和 pending receipt，再以 v0.6.0 打开。连续三次注册当前内置策略均得到：

```text
memory conflict: Steward profile version is immutable
```

诊断对照中，仅把新策略作为版本 2 注册并绑定，旧任务仍以原始版本 1 的 prompt 执行，新任务以版本 2 执行，两者都能接受确定性 ADD 提案。该对照只发生在临时库，不是建议下游接管内置版本命名空间。

影响限定为已经持久化旧内置策略的数据库。Open/迁移本身成功，基础 Remember/Recall 并未普遍不可用。issue 所述 Caelis 路径在策略同步失败后不进入 Runner，因此旧任务及依旧绑定入队的新任务会积压。本轮复现了 Memory 的冲突和任务快照行为；Caelis worker 停滞依据 issue 给出的固定提交控制流，未在本轮重新运行 Caelis 或真实 provider。

为什么已有测试漏报：

- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/testdata/v0.5.2/generate_test.go.txt:74` 使用自定义 `profile:migration`，没有覆盖真实内置策略。
- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/sdk/go/memory/stewardworker/policy_test.go:13` 断言版本为 1，并只检查当前 prompt 的部分内容，没有对照已发布的完整 ProfileSpec。
- 通用策略版本测试主动选择新版本，因此证明了机制正确，却不能证明内置策略遵守机制。

### F2 · P2：传递取消的 Steward 任务与公开回执处理状态不一致

位置：`/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/governance_cleanup.go:456`、`:470`；另一处需要同步审视的收尾逻辑在同文件 `:751`。

`invalidateForgettingClosure` 会把所有依赖上游证据的 pending/leased Job 标为 failed，但更新 `receipt_processing` 时只使用被治理的根 Receipt ID，没有更新被取消的下游 Job 所属 Receipt。

确定性复现过程：

1. 提交已确认事实 A。
2. 提交证据 B，由 Steward Claim B；其真实持久化读集合包含 A。
3. 分别执行删除 A 的证据、纠正 A 的证据。
4. 读取 B 的 `GetReceiptStatus`，并重放 B 的 `SubmitEvidence`。
5. 关闭、重新打开数据库，再重复读取。

两种操作在重启前后均得到：

```text
GetReceiptStatus = processing, terminal_error_code = ""
SubmitEvidence.Organization = failed
```

任务已不可继续执行，但依赖公开状态的宿主会持续展示处理中或等待完成。普通 Open 恢复不会修复已完成 barrier 留下的这项不一致。该复现不表示遗忘数据重新可读；问题是持久任务终态与公开状态不一致。

## 3. 整体质量判断与验收缺口

| 维度 | 本轮判断 |
| --- | --- |
| 独立嵌入与分层 | API、SDK、实现层归属明确；没有发现本次新增的 Caelis 产品类型侵入；独立消费模块门禁通过 |
| 授权与模型权限 | 候选查询按授权 Space/精确 LabelSet 选择；只读 facade 限制方法集；模型输出仍需 Memory 验证；现有隔离和 race 测试通过 |
| 事实语义 | 生命周期、确认、时间区间、条件优先级、异常覆盖与历史审计已有定向回归；本轮未再确认新的事实采用错误 |
| 治理 | 先提交屏障再清理、传递依赖、重启恢复有较强覆盖；F2 表明任务状态跨表一致性仍有遗漏 |
| 升级兼容 | schema 迁移测试不能替代真实发布策略和消费者启动路径测试；F1 是补丁发布的首要修复项 |
| 性能 | 现有证据主要覆盖同分区、不同主体的点查询；同主体长期积累与历史深度需要单独验收 |
| 模型/产品效果 | 尚不能从结构测试推导生产抽取、自然改写召回或最终回答质量 |

### Q1：返回预算不等于读取工作量预算

`/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/facts_read.go:106` 枚举匹配主体的 Record，并读取各自时间线；`:218` 在预算裁剪前逐项读取证据。达到 MaxFacts 后仍会继续处理后续候选。

现有 `/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/facts_performance_test.go:179` 每条事实使用不同主体，读取同时指定主体和键。这与一个长期用户积累许多事实后读取 Background 的形状不同。

本轮小规模诊断使用同一主体、不同键、短文本、每种查询 10 次、MaxFacts=8：

| 同主体事实数 | 指定键，中位数 | 不指定键，中位数 | 不指定键，最大值 |
| --- | --- | --- | --- |
| 100 | 0.127 ms | 5.802 ms | 6.060 ms |
| 1,000 | 0.129 ms | 56.539 ms | 57.663 ms |

这些是单机、非 race、小样本诊断值，不能外推成 100k 结果或可移植 SLO，也没有证明违反已声明的后台读取 SLO。它们支持补充真实工作负载验收，而不是认定现有 100k 报告已经覆盖该场景。

### Q2：结构正确性不等于生产模型质量

本轮 Facts 门禁通过：14/14 authored trajectories、208 个候选轨迹及其报告校验。候选只有 26 个模板、8 个主体，受控别名检查 448/448；不等于 208 个独立真实场景或任意自然语言改写召回达标。

生产模型抽取、遗漏、过期事实采用、Background 有用性和最终回答，仍按发布文档标为未验收。本轮没有调用模型。历史发布文档也明确记录最终完整性能复跑被停止，没有最终源码的完整聚合报告；本轮没有把旧结果重标成当前证明。

## 4. 可独立提交的修复切片

建议顺序：S1 → S3 → S4。S2 可独立完成，建议一并纳入 v0.6.1；若需要先止住升级阻塞，S1/S3/S4 可以先构成完整补丁，S2 随下一补丁发布。S5/S6 不阻塞紧急兼容性补丁。

### S1：修复内置策略版本并锁定已发布规格（v0.6.1，P1）

用户价值：已持久化 v0.5.2 或已发布 v0.6.0 内置策略的数据库，都能注册修复版当前策略。

文件责任：

- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/sdk/go/memory/stewardworker/policy.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/sdk/go/memory/stewardworker/policy_test.go`
- 计划新增 `/Users/xueyongzhi/WorkDir/caelis-labs/memory/sdk/go/memory/stewardworker/testdata/released_profiles/`

实现与兼容：

- 由 Memory 分配新的内置策略版本，当前发布源码只定义过版本 1，可采用 `memory-default@2`。
- 保留旧策略原文及已创建 Job 的版本引用；现有 Put/Bind 只改变未来任务。
- 保留“同 ID/version 不同内容返回 conflict”；不把任意冲突吞成成功，不自动覆盖版本 1，不让下游手工修改内置版本。
- 冻结已发布的完整 ProfileSpec（包括 prompt、上下文/输入/输出预算），记录来源 tag/SHA。v0.5.2 与 v0.6.0 已经存在的两份版本 1 规格必须分别保留为历史事实，不能伪造为同一份。
- 新测试要求当前规格与该版本的已发布快照一致；任何规格变化必须分配新版本并添加明确快照。不要只与本轮 `BuiltInProfile()` 相互比较。
- 单独修复策略碰撞不需要数据库 schema 升级，也不修改已发布模块标签。

验收：两种历史规格都能与版本 2 共存；新库和重复注册幂等；同键不同内容仍拒绝；旧任务快照未变。

建议 commit：`fix(steward): version the changed built-in profile`。

### S2：统一治理取消任务与回执终态（建议 v0.6.1，P2）

文件责任：

- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/governance_cleanup.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/governance_cleanup_test.go`
- 必要的初始化修复入口：`/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/store.go`

实现与兼容：

- 在取消 Job 的同一事务中，将实际被取消 Job 所属的所有存活 Receipt 的 accepted/processing 状态改为 failed，写入对应终态原因。
- 提取或复用统一收尾逻辑，覆盖屏障事务和 cleanup/recovery 路径，避免两套状态更新再次分叉。
- 不改写 completed Job 或 organized Receipt，不自动重新入队被治理取消的工作，不跨 Space/LabelSet 扩展影响范围。
- 对 v0.6.0 已持久化的坏状态增加明确、幂等的数据修复步骤：仅依据失败 Job、已提交治理证据及已知取消原因修正匹配的非终态回执。文档记录触发条件和范围；不能全表把 processing 清成 failed。
- 此项数据修复与 S1 的策略版本递增分开设计，不因策略变更引入无关 schema 字段。

验收：delete/correct × leased/pending-retry × 立即读取/重启；`GetReceiptStatus`、幂等 Remember、SubmitEvidence.Organization 与 Job 状态一致；完成任务和无关分区保持正确状态；故障注入后可恢复，旧 lease 仍不能 Apply。

建议 commit：`fix(governance): settle dependent receipt processing state`。

### S3：把真实跨版本升级接入公共消费门禁（v0.6.1 发布前，P1）

依赖 S1。该切片交付可持续执行的跨发布测试，不只保留手工复现说明。

文件责任：

- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/facts_migration_test.go`
- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/testdata/`
- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/scripts/facts_consumer_gate/main.go`，或新增独立的升级 consumer gate
- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/Makefile`
- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/.github/workflows/quality.yml`

矩阵：

| 起点 | 必须验证 |
| --- | --- |
| 真正由 v0.5.2 生成，真实内置 v1 + active binding + pending work | Open → Put 当前策略 → Bind → 旧任务及新任务经确定性 Runner 完成 |
| 真正由已发布 v0.6.0 新建，另一份内置 v1 | 同样升级成功；该 v1 保持原文 |
| 修复版新库 | 注册、绑定、执行、重启和重复注册幂等 |
| 自定义 profile / 同键不同内容 | 保持原有自定义策略与冲突语义，不误识别或覆盖 |
| v0.5.2 曾 lease 的工作 | 迁移后按现有规则重新 Claim、重建读集合，旧 lease 无权发布 |

夹具由准确 release 源码生成，提交生成程序、tag/SHA 和数据库哈希；不能让新版本模拟旧 schema 或用自定义 profile 代替旧内置策略。消费者部分只导入 `appliance`、版本化 API 与 SDK，以确定性 ModelGenerator 驱动公开 Runner，不导入 internal，不调用真实 provider。

验收必须断言 Receipt 数量/身份、旧 profile 内容、binding 权限、Job 快照和最终状态，而不只是 Open 返回 nil。S2 的持久坏状态另有明确升级修复夹具。

建议 commit：`test(upgrade): gate released-profile and queued-work compatibility`。

### S4：补丁发布说明与下游恢复验收（v0.6.1 发布闭环）

文件责任：

- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/docs/memory-v0.6-migration.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/docs/memory-v0.6-release.md`
- `/Users/xueyongzhi/WorkDir/caelis-labs/memory/docs/memory-appliance-release.md`
- release-please 管理的 CHANGELOG/VERSION/manifest

说明受影响人群、可观察症状、补丁恢复步骤和前向迁移边界。对已迁移至 schema 2 的库，不建议仅回退依赖/二进制；需要回退时使用已验证的升级前备份和既有恢复流程。补丁不得重新打 v0.6.0 标签。

Memory 发布后，Caelis 在自己的仓库升级到实际发布版本，使用原有 Put/Bind 选择策略；更新“内置策略永远是版本 1”的测试假设。以“旧库 + Steward 开启 + pending work”证明 worker 真正运行并排空工作，而不只验证 Host 启动和新库 Golden Path。该产品验收不能反向向 Memory 引入 Caelis 类型。

发布门禁：

```sh
make check
GOWORK=off go test -race ./...
make durable corpus-gate facts-gate facts-consumer-gate
# 加入 S3 的升级门禁
git diff --check
```

按仓库既有 release 流程保留精确源码身份和各平台结果；正式发布后的外部模块消费再核验真实版本，不把本地 replace smoke 当成正式模块升级成功。issue #5 的关闭以其验收清单完成为准。

建议 commit：`docs(release): document v0.6.1 upgrade recovery and qualification`。

### S5：同主体长期积累的性能修复与验收（后续补丁，P2）

文件责任：`/Users/xueyongzhi/WorkDir/caelis-labs/memory/internal/appliance/facts_read.go`、`facts_performance_test.go`，以及必要的 focused tests 和性能文档。

先冻结同主体多键、同键深历史、条件/异常并存的 100/1k/10k 工作负载，再比较点查询和 Background。增加 SQLite 查询数、分配量、取消响应、并发读写及正确性断言，预先定义本机阈值。

优化优先消除结果名额已满后的逐候选证据加载，分离判定采用关系所需的数据与最终返回 payload 的加载。不能为了减少工作而在异常覆盖、条件歧义、历史区间解析前任意 LIMIT 候选；这会重新引入旧事实采用错误。若需要索引/数据迁移，另写明确迁移及其重启验收，不引入向量或图数据库。

验收：已有语义回归全部通过；输出、Background、RefreshAt、Cursor、Truncated 等约定不变；新工作负载有修复前后同源报告，不能用提高阈值代替改善。

建议 commit：`perf(facts): avoid unnecessary payload reads for bounded backgrounds`。

### S6：生产模型与消费质量单独验收（后续质量里程碑）

文件责任：`/Users/xueyongzhi/WorkDir/caelis-labs/memory/docs/memory-v0.6-evaluation.md` 及独立评测夹具/runner；消费者回答质量由各宿主负责。

冻结与受控别名表独立的自然语言 holdout、模型/profile/预算/提示词版本及判定准则。覆盖遗漏、否定、条件、临时例外、过期事实、其他主体和模型诱导。分别报告 extraction、adoption、retrieval 与最终回答，记录实际调用与失败，而不是将一个结构测试百分比替代所有质量维度。

本项不要求为修复 issue #5 更换模型或扩大 Memory 职责；生产质量未实测前继续保持现有未验收声明。

## 5. 本轮验证记录

环境：Go 1.26.8、darwin/arm64、CGO_ENABLED=1，独立模块模式 GOWORK=off。

| 项目 | 实际结果 |
| --- | --- |
| `GOWORK=off go test -race ./...` | 全部通过 |
| `make check` | 链接、格式、空白、全量测试、vet、build、diff 检查通过 |
| `make facts-gate` | 14 authored + 208 candidate，报告校验通过 |
| `make facts-consumer-gate` | 临时独立 Go 模块消费通过 |
| v0.5.2 真实内置策略升级复现 | 三次 immutable conflict；新策略版本诊断对照可完成旧/新任务 |
| 传递治理状态回归 | delete/correct 两个用例均失败，重启后错误保持 |
| 同主体读取诊断 | 100/1k、点读/Background 各 10 次，见 Q1 |
| Linux/Windows 原生、Go 最低版本 CGO=0 | 本轮未重跑，不把本机结果作为跨平台证明 |
| 100k 完整性能/soak、真实模型、Caelis 当前完整集成 | 本轮未执行 |

最初沙箱运行因 Go 构建缓存权限失败；随后在获准环境重跑成功，该环境失败未计作代码缺陷。

复现源码和观测记录保存在 [证据目录说明](evidence/memory-v0.6-review-2026-09-21/README.md)。这些 `.go.txt` 文件是审查证据，不自动加入仓库测试套件；S1/S2/S3 应把对应行为转为正式回归测试。
