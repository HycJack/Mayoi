# Mayoi AI 助手设计（基于 pi-ai-go）

日期：2025-06-27
状态：设计已获用户批准，待写实施计划

## 目标

给 Mayoi 加一个内置 AI 编码助手：右侧可开合面板里流式对话，agent 用工具读写代码、
执行搜索，写操作先经用户审批、以 diff 展示。对话、工具循环、上下文压缩、会话持久化
全部由本地 pi-ai-go 库（`github.com/HycJack/pi-ai-go`）驱动，Mayoi 只做 UI、工具接线和
审批闸门。多 provider（Anthropic/OpenAI/Google/Bedrock/DeepSeek/GLM/Kimi/Mistral/
OpenRouter 等）随 pi-ai-go 自动获得。

非目标：不替换现有 Mon 网关的 AI commit message 功能；不引 webview；第一版不做
子代理与 todo 工具。

## 已确认的决策

- 范围：全量移植——完整助手 + 多会话历史持久化 + 五个内置人格 + MAYOI.md 项目记忆。
- 密钥：API key 存 JSON（明文），但**单独放 `ai-keys.json`**，不进设置主文件，
  因为设置主文件会被 cloudsync 同步上云。
- 依赖：go.mod `replace` 指向本地 pi-ai-go 仓库（`../../pi-ai-go`，相对模块路径，
  换机器只需本地有该 checkout）。
- UI：右侧边面板（与左侧侧栏对称），可拖宽、状态入 window layout。

## 1. 依赖接入

```
require github.com/HycJack/pi-ai-go v0.0.0
replace github.com/HycJack/pi-ai-go => ../../pi-ai-go
```

新增 `internal/ai` 包，是 Mayoi 内**唯一**允许 import pi-ai-go 的地方。
模型目录直接读 pi-ai-go 的 `llm`（`models_generated.go`）。

## 2. 设置与密钥

`Prefs` 新增 `AI` 组（同步上云，非敏感）：

- `AI.Provider`、`AI.Model` — 当前模型
- `AI.Persona` — 当前人格 id
- `AI.ApproveEdits` — 写审批开关（默认开）
- `AI.AllowBash` — bash 工具开关（默认关）
- `AI.SystemExtra` — 用户自定义追加指令

密钥文件 `ai-keys.json`（配置目录下，明文、手改即生效、不走同步）：
`{"anthropic": "...", "openai": "...", ...}`，provider id 为键。
现有 `Prefs.AIModel`（Mon 网关）不动。

## 3. Agent 编排与工具

用 pi-ai-go `agent.New` + 事件流驱动一次运行（上限 24 步）：

| 事件 | UI 呈现 |
|---|---|
| `EventMessageUpdate` | 流式文本增量 |
| `EventToolExecStart/End` | 工具调用卡片（名称 + 参数摘要 + 结果折叠） |
| `EventCompaction` | 一条"上下文已压缩"提示 |
| `EventAgentEnd` | 收尾、落盘、触发自动标题 |

工具三档：

1. **直接复用** pi-ai-go 的 `read`、`glob`、`grep`（只读，工作区根路径约束不变）。
2. **包审批** 的 `write`、`edit`：执行前回调 Mayoi，弹审批卡片（linediff 生成 diff，
   diffview 同款渲染，Approve/Reject）。批准后写入走 Mayoi vfs，已打开标签页实时刷新；
   拒绝则把"用户拒绝修改"作为工具结果回给模型。`AI.ApproveEdits` 关闭时跳过审批直接写。
3. **默认关** 的 `bash`：受 `AI.AllowBash` 控制，开启时每次执行也要过审批卡片。

另加一个 Mayoi 专属工具 `open_file`（path, line?）：在编辑器里打开文件定位到行，
供 AI 讲解时带用户看代码。

## 4. 右侧面板 UI

- 右侧可开合 AI 面板，宽度可拖，开合与宽度存 window layout，快捷键 ⌘. / Ctrl+.。
- 结构：顶部会话切换下拉 + 新建会话按钮；中部消息流（用户消息、流式助手文本、
  工具卡片、审批卡片内嵌 diff）；底部输入框（Enter 发送、Shift+Enter 换行、Esc 中断运行）。
- 全部 mygo 即时模式绘制，与现有 sidebar/panel 同风格。

## 5. 会话历史

- pi-ai-go `agent/session`（JSONL 会话树），每会话一个文件存配置目录 `ai-sessions/`。
- 会话列表自动标题：首轮结束后用当前模型生成短标题，失败回退为首句前 40 字符。

## 6. 人格与项目记忆

- 五个内置人格（coder/architect/reviewer/security/designer）提示词原文移植自
  terax-clone `frontend/src/modules/ai/lib/agents.ts`，存 `internal/ai/personas.go`。
- 自定义人格存设置 JSON（id、name、instructions），运行时与内置合并。
- `MAYOI.md`：工作区根读取，注入 system 提示（30s 缓存、32KB 截断）。
- env 注入：活动终端 cwd、workspace 根、活动文件路径，随最后一条 user 消息以
  `<env>` 块附加。

## 7. 错误处理

- 无 key / key 无效：面板顶部横幅提示，点击跳设置。
- 流中断/超时：pi-ai-go 已有 retry；仍失败则消息流里落错误卡片，Esc 可随时取消。
- 磁盘会话写失败：面板提示但不清空内存对话。

## 8. 测试策略

- `internal/ai`：单测覆盖 provider 配置解析、key 文件读写、工具参数校验、
  人格合并、MAYOI.md 截断。agent 循环用 pi-ai-go 的 `faux` provider 驱动端到端测试。
- `cmd/mayoi`：沿用现有 mygo 假 app 模式，测面板开合、审批卡片进出、
  会话切换的状态流转；审批 → vfs 写入 → 标签页刷新链路用临时目录测。

## 9. terax-clone 来源映射

实施时从 `C:\Users\huangyicao\Downloads\terax-clone` 搬运的对照表（其前端 TypeScript
编排已被 pi-ai-go 取代，只搬数据资产与机制设计）：

| Mayoi 位置 | 搬什么 | terax-clone 来源 |
|---|---|---|
| `internal/ai/personas.go` | 五个人格的 `instructions` 原文 | `frontend/src/modules/ai/lib/agents.ts` 的 `BUILTIN_AGENTS` |
| `internal/ai`（自定义人格） | 人格增删/合并机制设计 | `agentsStore.ts` 与设置界面逻辑，数据结构改存 Mayoi 设置 JSON |
| `internal/ai`（MAYOI.md 注入） | 项目记忆机制：30s 缓存、32KB 截断 | `lib/transport.ts` 的 `ContextAwareTransport`（Go 重写） |
| `internal/ai`（env 注入） | `<env>` 块内容清单：终端 cwd、workspace 根、活动文件 | 同上 `transport.ts` |
| `internal/ai`（审批机制） | 写操作 approve/reject、拒绝结果回给模型、审批后开 AI diff 标签页 | `tools/*` 的 read-before-edit 与审批策略、`docs/agent-and-conversation-design.md` §2.2 |
| `internal/ai`（自动标题） | 首轮后小模型生成标题、失败取首句前 40 字 | `chatStore.ts` 的会话标题逻辑 |
| 会话历史 | 多会话列表 + 持久化（实现改用 pi-ai-go 的 session JSONL） | `chatStore.ts` + `internal/store` |
| 不搬 | PTY 终端、CodeMirror、LSP、文件树/grep/Git、React 组件库 | Mayoi 已有或更强，见对比结论 |

运行链路参考：terax-clone `docs/agent-and-conversation-design.md`（24 步上限、
工具系统分组、上下文压缩均出自该文档，与 pi-ai-go 能力一一对应）。

## 10. 里程碑

1. **M1 基础聊天**：依赖接入 + `internal/ai` 骨架 + 右侧面板 + 流式对话（无工具）
2. **M2 Agent 工具**：三档工具 + 写审批 diff 卡片 + vfs 联动 + open_file
3. **M3 会话持久化**：session JSONL + 会话列表 + 自动标题
4. **M4 人格与记忆**：人格切换 + 自定义人格 + MAYOI.md + env 注入

每步完成后跑全量测试 + 补新测试，CHANGELOG 按 AGENTS.md 规则加条目。

## 11. 与本文的差异（实施后的实况）

M1–M4 均已完成并合入，以下为实施中与设计不同、或尚未做的地方：

| 设计 | 实况 |
|---|---|
| §2 `AI.SystemExtra` | 已做，且在 **AI Settings** 对话框里可视编辑 |
| §2 密钥手改 `ai-keys.json` | 保留手改即生效，另加 **AI Settings** 里填 key（写 `0o600`，不进同步） |
| §3 工具卡片显示参数与可折叠结果 | 只显示工具名；`bash` 显示命令、写操作显示行级 diff —— 结果常常很大，折叠进面板会让消息流难读 |
| §3 `EventCompaction` 提示“上下文已压缩” | 未做：pi-ai-go 的 `agent` 事件流没有这个事件（`core` 只有 `CompactionCancelledError`），等上游 |
| §4 快捷键 ⌘. / Ctrl+. | 改用 `Alt+CmdOrCtrl+A`（`⌘.` 在 macOS 上被系统输入法切换占用），另加命令面板与 **View → AI Settings** |
| §5 首轮后用模型生成会话标题 | 不调模型：直接取首句前 40 字。省一次往返；要改回只需在 `keepChat` 里加一次调用 |
| §5 会话只存文本 | 已改为存模型视角的完整对话（含工具调用与结果），重开会话即同一个助手 |
| §6 自定义人格只能改设置 JSON | 已做 **AI Settings** 内的增删改；`ai.personas` 手写仍生效 |
| §7 无 key 时“点击跳设置” | 面板错误行旁有 **AI Settings…** 按钮（任何失败都显示，不只是无 key） |
