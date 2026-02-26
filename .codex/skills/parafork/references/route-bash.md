# ROUTE: Bash（执行抄写板）

适用环境：Linux / macOS / WSL / Git-Bash。

## 路径约定
- `<PARAFORK_ROOT>`：skill 根目录
- `<PARAFORK_BASH_SCRIPTS>`：`<PARAFORK_ROOT>/bash-scripts`
- 入口模板：`bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" <cmd> [args...]`

## 1) 默认入口
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh"`

## 2) 手动命令
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" help --debug`
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" init --new`
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" init --reuse --yes --i-am-maintainer`
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" do exec`
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" do exec --strict`
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" do commit --message "..."`
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" do commit --message "..." --no-check`
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" check status`
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" check merge`
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" check merge --strict`

## 3) Merge
- `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" merge --yes --i-am-maintainer`

## 4) 锁冲突
- `SAFE_NEXT`（默认）：`bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" init --new`
- `TAKEOVER_NEXT`（仅获批后）：
  - `cd "<WORKTREE_ROOT>"`
  - `bash "<PARAFORK_BASH_SCRIPTS>/parafork.sh" init --reuse --yes --i-am-maintainer`
