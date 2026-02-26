# Scripts（命令矩阵）

## 入口
- Bash：`bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" <cmd> [args...]`
- PowerShell：`powershell -NoProfile -ExecutionPolicy Bypass -File "<PARAFORK_POWERSHELL_SCRIPTS>\\parafork.ps1" <cmd> [args...]`

## base-allowed
| 命令 | 允许参数 |
|---|---|
| `help` | `debug` / `--debug` |
| `init` | `--new` |

> 无参默认入口：`init --new` + `do exec`。

## worktree-required
| 命令 | 允许参数 |
|---|---|
| `init --reuse` | `--yes --i-am-maintainer` |
| `do exec` | `--strict` |
| `do commit` | `--message "<msg>" [--no-check]` |
| `check status` | 无 |
| `check merge` | `[--strict]` |
| `merge` | `--yes --i-am-maintainer` |

## 兼容性
- 仅支持 Core-Lite：`help/init/do/check/merge`。
- 旧命令均不支持（例如 `watch`、`do pull`、`check diff/log/review`）。
