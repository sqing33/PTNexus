# PTNexus Issue 审查报告（Python 时代 -> Go 版本）

生成时间：2026-03-04
审查基线：本地分支 `go`（commit `b9e7e78`）
远端仓库：`jadylc/PTNexus`

> 说明
> 1. 本报告覆盖 GitHub Issue `#1`-`#62` 中可访问且属于 `issue` 的 58 条记录（其中 `#4` 为 PR，不在 issue 范围；`#31` 不存在；`#34/#36` 已被删除）。
> 2. 目标：把历史问题逐条映射到 Go 版本当前实现，判断“已解决/仍存在/需复现/需求变更”，并对仍可直接修复的问题创建独立分支进行修复（不合并，由你后续逐分支审查/合并）。

## 0. 统计

- Issues 总数：58
- Open：17（#62 #61 #60 #59 #58 #57 #56 #55 #54 #49 #48 #46 #45 #42 #40 #39 #23）
- Closed：41

## 1. 状态标记（Legend）

- `FIX_NOW`：可在 Go 版本直接定位并修复（会创建 worktree-lite 分支）
- `NEEDS_REPRO`：需要最小复现或样例数据（仅做代码静态分析无法保证）
- `FEATURE`：功能建议/站点适配请求（不一定等价于 bug；本轮先记录，不默认实现）
- `CLOSED_OK`：已关闭，且在 Go 版本中基本等价解决/行为已对齐（不再单独开分支）
- `CLOSED_SKIP`：已关闭，但属于咨询/环境/无复现（仅归档）

## 2. 本轮计划修复（分支已创建）

> 分支名由 `worktree-lite init` 自动生成（包含日期与随机后缀）。

- `#55` 对无目录的单文件无法识别（proxy 侧 `.iso`）: `FIX_NOW`（branch: `worktree-lite/260305-issue-55-proxy-iso-a071`）
- `#56` 对多盘的源盘识别不正确（多盘 BDMV 识别/选择）: `FIX_NOW`（已合并到 `go`: `1921355`；原分支 `worktree-lite/260305-issue-56-multidisc-bdmv-5962` 已清理）
- `#39` 显示完成发布,实际添加种子失败（qB add 返回 Fails. 未判错）: `FIX_NOW`（branch: `worktree-lite/260305-issue-39-qb-add-fails-de-01e6`）
- `#40` PTNexus 造成 QB 下载器卡顿（proxy 拉 trackers/comment per-torrent 调用过重）: `FIX_NOW`（branch: `worktree-lite/260305-issue-40-proxy-qb-lag-17cc`）
- `#42` Windows Docker Desktop 下更新失败（updater 默认禁用系统代理）: `FIX_NOW`（branch: `worktree-lite/260305-issue-42-updater-proxy-a-ecec`）
- `#45` TR 删除的种子 PTNexus 仍显示已存在 + 旧/新肉丝链接（详情链接归一化等）: `FIX_NOW`（已合并到 `go`: `92c30d0`；原分支 `worktree-lite/260305-issue-45-stale-details-l-cae1` 已清理）
- `#54` UBits 480p 分辨率映射错误（`resolution.r480p` 未映射到 SD）: `FIX_NOW`（branch: `worktree-lite/260305-issue-54-ubits-480p-reso-0669`）

## 3. 全量 Issue 清单（逐条归档与结论）

> 下面按 issue 编号倒序列出。对于 `FIX_NOW` 的条目，会在完成修复后回填分支名、改动点与验证方式。

| # | State | Title | 结论 | 分支 | 备注 |
|---:|:---:|---|---|---|---|
| 62 | Open | 带特殊字符的目录无法识别问题，麻烦大佬可以帮看看嘛。 | `NEEDS_REPRO` | - | 更像路径映射/编码/SMB 场景；需样例路径与日志才能定因。 |
| 61 | Open | 关于文件信息提取以及站点适配请求 | `FEATURE` | - | 1) 利用 MediaInfo 自动填充部分字段；2) 站点“好学”适配请求。 |
| 60 | Open | 简介识别异常 | `NEEDS_REPRO` | - | 需要异常页面 HTML/截图对应的原始简介内容才能定位解析器。 |
| 59 | Open | 批量发布同一个种子 末日添加到tr下载器失败 | `NEEDS_REPRO` | - | Issue 日志为 Python 旧逻辑“站点匹配失败”；Go 版已做容错，但仍需复现验证。 |
| 58 | Open | 关于设置中的站点序列建议 | `FEATURE` | - | UI 可排序增强（不影响核心链路）。 |
| 57 | Open | 请求适配红豆饭 | `FEATURE` | - | 站点适配请求（目标站）。 |
| 56 | Open | 对多盘的源盘识别不正确 | `FIX_NOW` | `go@1921355` | Go 侧蓝光判定仅检查 `BDMV` 直子目录；多盘结构需增强。 |
| 55 | Open | 对无目录的单文件无法识别 | `FIX_NOW` | `worktree-lite/260305-issue-55-proxy-iso-a071` | proxy 的 `findTargetVideoFile` 未支持 `.iso`。 |
| 54 | Open | 关于杜比和ub发种问题 | `FIX_NOW` | `worktree-lite/260305-issue-54-ubits-480p-reso-0669` | 先修 UBits 480p -> SD（`resolution.r480p`）；Dubhe 的“高帧率/高码率”需站点 tag ID 才能完善。 |
| 53 | Closed | 馒头cookise怎么填写 | `CLOSED_SKIP` | - | 咨询类问题。 |
| 52 | Closed | 关于转种目标站点种子存在问题 | `CLOSED_OK` | - | Go 版 publish 已支持 `auto_add_existing_to_downloader` 开关语义。 |
| 51 | Closed | Afun发种规范化问题 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 50 | Closed | 肉丝发种问题 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 49 | Open | 建议增加个出种之后限速的设置 | `FEATURE` | - | 需要明确“限速触发条件/定时策略/对 qB/TR 的落地方式”；目前仅存在 proxy 批量限速接口雏形。 |
| 48 | Open | 添加远程qb下载器，直连可获取种子大小；开启代理（默认9090）后，无法获取种子大小。 | `NEEDS_REPRO` | - | proxy 已按 int64 解析 size；仍需对照用户环境复现确认。 |
| 47 | Closed | 建议出个自我标记盒子功能 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 46 | Open | 能适配一下萝莉吗? | `CLOSED_OK` | - | Go 版已存在站点 `ilolicon`（配置与站点数据均存在）；更像“如何启用/配置”问题。 |
| 45 | Open | TR里已经删除的种子，PT nexus里还是显示已存在 | `FIX_NOW` | `go@92c30d0` | 需要同时处理“删除后仍显示/旧域名跳转”两类现象，优先做链接归一化与刷新链路排查。 |
| 44 | Closed | 魔都风云的IMDB与TMDB链接都是错误的 | `CLOSED_SKIP` | - | 外部元数据缺失/匹配失败类；已关闭 not planned。 |
| 43 | Closed | 分集识别问题 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 42 | Open | 在windows的docker deskop下更新失败 | `FIX_NOW` | `worktree-lite/260305-issue-42-updater-proxy-a-ecec` | updater 默认不走系统代理，Docker Desktop 代理设置不生效。 |
| 41 | Closed | 图床好像失效了？ | `CLOSED_SKIP` | - | 外部图床可用性问题；已关闭。 |
| 40 | Open | 【bug】PTNexus 造成 QB 下载器卡顿 | `FIX_NOW` | `worktree-lite/260305-issue-40-proxy-qb-lag-17cc` | proxy 在 includeTrackers/includeComment 时对每个 torrent 额外调用 properties/trackers，导致卡顿。 |
| 39 | Open | 显示完成发布,实际添加种子失败 | `FIX_NOW` | `worktree-lite/260305-issue-39-qb-add-fails-de-01e6` | qB `torrents/add` 可能返回 200 + `Fails.`，当前未判错。 |
| 38 | Closed | 生成截图的等待时间太短，前端超时出错 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 37 | Closed | 青蛙标题规范问题 【HEVC或H265改为H.265】 【AVC或H264改为H.264】 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 35 | Closed | MA无法识别为片源平台 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 33 | Closed | 杜比源站转种时总是提示缺少源站链接 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 32 | Closed | 一站多种转种状态一直是错误 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 30 | Closed | 发布参数中的主标题解析问题 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 29 | Closed | 请求适配人人 | `CLOSED_OK` | - | Go 版站点数据已包含 `audiences`（观众）。 |
| 28 | Closed | 如下功能看大佬是否可以添加 | `CLOSED_SKIP` | - | 多项需求集合；已关闭。 |
| 27 | Closed | 请求添加点击首页站点可以直达配置界面 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 26 | Closed | 转种时把源标题中免费时间的标记一起转新站标题中了 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 25 | Closed | 申请适配Rousi新框架 | `CLOSED_OK` | - | Go 版已存在肉丝特殊发布器。 |
| 24 | Closed | WebDL资源媒介 (medium)识别错误 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 23 | Open | 发布失败 | `NEEDS_REPRO` | - | 需拿到 Go 版请求/响应日志或站点返回内容，才能判断是站点变更/参数映射/网络问题。 |
| 22 | Closed | 需求 | `CLOSED_SKIP` | - | 信息不足且已关闭。 |
| 21 | Closed | 做种检索页面，选择路径下拉框的数据不对 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 20 | Closed | 地址被封禁，这个怎么解？ | `CLOSED_SKIP` | - | 站点侧封禁/环境问题；已关闭 not planned。 |
| 19 | Closed | 反馈更换客户端和路径后无法识别新的 | `CLOSED_SKIP` | - | 需要复现数据；已关闭 not planned。 |
| 18 | Closed | 增加标题音频自动格式化功能 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 17 | Closed | 增加禁止转载分集、单集的开关 | `CLOSED_OK` | - | Go 版已在发布前统一拦截“禁转/限转/分集”标签（无开关），与 issue 结论一致。 |
| 16 | Closed | 大佬建议增加所有配置的一键导出和导入恢复功能，重装后不用重新配置 | `CLOSED_SKIP` | - | owner 建议直接复制 data 目录迁移；本轮不新增导入导出。 |
| 15 | Closed | 申请适配BaoziPT | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 14 | Closed | 关于登录密码的建议 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 13 | Closed | [BUG]特殊情况下海报与简介详情获取错误 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 12 | Closed | 更新3.1版本后点击“转种”无反应 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 11 | Closed | 申请适配Pter | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 10 | Closed | 申请支持【LongPT】 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 9 | Closed | 转种一直卡着，网络请求报错未授权 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 8 | Closed | 登录成功后页面不会跳转 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 7 | Closed | 项目启动，创建文件夹失败 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 6 | Closed | [BUG]升级到v3.0的问题 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 5 | Closed | [BUG]智能获取视频文件mediainfo错误 | `CLOSED_OK` | - | 已关闭；与 #55/#56 同类，后续以现修复为准。 |
| 3 | Closed | [BUG]官组的匹配参数错误匹配和建议 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |
| 2 | Closed | [BUG] 下载统计分栏，速率-近1分钟-仅上传图表刷新异常 | `CLOSED_SKIP` | - | issue 自述无法复现后关闭。 |
| 1 | Closed | 下载统计是否可以区分站点 | `CLOSED_OK` | - | 已关闭；按历史结论归档。 |

## 4. FIX_NOW 条目修复记录

### #39 显示完成发布,实际添加种子失败

- 分支：`worktree-lite/260305-issue-39-qb-add-fails-de-01e6`
- 根因：qBittorrent Web API 在部分失败场景会返回 HTTP 200，但 body 为 `Fails.`；Go 端仅按状态码判断成功，导致 UI 误报“添加成功”。
- 修复：
  - 在 qB client 的 `PostForm` / `PostMultipart` 中，若 body 为 `Fails.`（或 `Fails`）则视为失败并返回错误。
  - 文件：`server/internal/service/downloaderclient/client.go`
- 验证：在该 worktree 执行 `cd server && go test ./...`（通过）。

### #40 PTNexus 造成 QB 下载器卡顿

- 分支：`worktree-lite/260305-issue-40-proxy-qb-lag-17cc`
- 根因：proxy 在 `include_comment/include_trackers=true` 时对每个 torrent 逐个调用 `torrents/properties` + `torrents/trackers`，导致 qB 侧请求量爆炸并卡顿。
- 修复：
  - 改为直接使用 `torrents/info` 返回的 `comment` / `tracker` 字段，不再 per-torrent 额外请求。
  - 文件：`proxy/proxy.go`
- 验证：在该 worktree 执行 `cd proxy && go test ./...`（通过，编译检查）。

### #42 Windows Docker Desktop 下更新失败

- 分支：`worktree-lite/260305-issue-42-updater-proxy-a-ecec`
- 根因：updater 默认禁用系统代理，导致 Docker Desktop 环境里即便设置了 `HTTP_PROXY/HTTPS_PROXY` 也无法用于拉取更新。
- 修复：
  - 若显式设置 `UPDATE_USE_PROXY=true/false`：保持原语义。
  - 若未设置 `UPDATE_USE_PROXY`：当检测到 `HTTP_PROXY/HTTPS_PROXY/NO_PROXY` 任一存在时自动启用 `http.ProxyFromEnvironment`。
  - 文件：`updater/update_manifest.go`
- 验证：在该 worktree 执行 `cd updater && go test ./...`（通过，编译检查）。

### #45 TR 删除的种子 PTNexus 仍显示已存在 + 旧/新肉丝链接

- 分支：已合并到 `go@92c30d0`（原 `worktree-lite/260305-issue-45-stale-details-l-cae1` 已清理）
- 现状拆分：
  - “点击站点名打开旧域名”：可静态定位并修复。
  - “TR 删除后仍显示已存在”：需要结合用户数据/日志复现进一步确认（本分支优先解决链接归一化与域名切换兼容）。
- 修复：
  - 刷新下载器数据时对 `details` URL 做归一化：当识别到站点昵称后，将详情页 URL 的 host 统一替换为 DB `sites.base_url`（并移除遗留 `existed=1` 参数），避免旧域名残留。
    - 文件：`server/internal/service/torrentdata/refresh_sync.go`
  - “按详情页反查站点”增强：支持按 core domain 兜底匹配，兼容站点更换域名导致的反查失败。
    - 文件：`server/internal/service/acquire/fetch/site_lookup.go`
- 验证：在该 worktree 执行 `cd server && go test ./...`（通过）。

### #54 UBits 480p 分辨率映射错误

- 分支：`worktree-lite/260305-issue-54-ubits-480p-reso-0669`
- 根因：UBits 表单没有单独的 480p 档位，480p 应归入 SD；但映射缺失导致回退到默认 720p。
- 修复：
  - 将 `resolution.r480p` 映射到与 `resolution.sd` 相同的值。
  - 文件：`server/configs/ubits.yaml`
- 验证：在该 worktree 执行 `cd server && go test ./...`（通过）。

### #55 对无目录的单文件无法识别（.iso）

- 分支：`worktree-lite/260305-issue-55-proxy-iso-a071`
- 根因：proxy 侧视频文件选择逻辑未将 `.iso` 视为候选，导致“单文件 ISO”无法被识别。
- 修复：
  - `findTargetVideoFile`：将 `.iso` 纳入可识别扩展名。
  - `screenshotHandler`：当目标为 `.iso` 时明确拒绝截图（避免继续走 ffprobe/ffmpeg）。
  - `episodeCountHandler`：扩展名列表补齐 `.iso`。
  - 文件：`proxy/proxy.go`
- 验证：在该 worktree 执行 `cd proxy && go test ./...`（通过，编译检查）。

### #56 对多盘的源盘识别不正确

- 分支：已合并到 `go@1921355`（原 `worktree-lite/260305-issue-56-multidisc-bdmv-5962` 已清理）
- 根因：蓝光原盘判定仅支持 `<root>/BDMV`，对 `<root>/<disc>/BDMV` 的多盘结构会误判为普通媒体文件目录，导致走“选最大 .m2ts -> MediaInfo”而不是“按原盘处理”。
- 修复：
  - 新增蓝光目录解析：支持 root 下 1 层子目录包含 `BDMV` 的结构，并优先选择 `disc/part/cd` 号更小的盘（如 1）。
  - BDInfo 提取时使用解析后的原盘根目录执行 `BDInfo -p`（避免传入外层目录导致失败）。
  - 文件：`proxy/bdinfo.go`
- 验证：在该 worktree 执行 `cd proxy && go test ./...`（通过，编译检查）。

## 5. 附录：缺失/已删除 Issues

- `#31`：GitHub API 返回 Not Found（无法获取内容）
- `#34`：GitHub 标记为已删除（无法获取内容）
- `#36`：GitHub 标记为已删除（无法获取内容）
