# 运维留痕

本目录记录本 fork（`ganzizi/grok2api`）线上实例的排查结论，方便下次对照，不是上游功能说明书。

合上游前先读 **[fork-delta.md](./fork-delta.md)**：必须保留的行为差、热冲突文件、回归测试、禁止项。不要靠猜 merge。

| 日期 | 文档 | 摘要 |
| --- | --- | --- |
| 常驻 | [本 fork 相对上游的修改清单](./fork-delta.md) | 下次升级对照：compact skip、pin 换号、fail_closed、热文件、测试 |
| 2026-09-18 | [Claude Code 压缩 skip 与审计「200 · 错误」](./2026-09-18-claude-code-compact-skip-and-200-error.md) | New API compact 走 chat 也要 skip hold；「200 · 错误」多半是质量影子审计 |
| 2026-09-12 | [免费额度 429、会话 pin 换号、质量探测噪音](./2026-09-12-free-quota-429-pin-failover.md) | 长任务死掉 ≠ 后台一直刷 429；A 修 pin，探测每 5 分钟自己报 429 |

写新笔记时：不写密钥、SSH 密码、账号邮箱；实例用栈名（例如 `ganzizi-grok2api-15353`）即可。
