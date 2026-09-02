# gpu-lan-runtime 安全审计与 Skill 安装报告

- **日期**：2026-09-03
- **审计对象**：https://github.com/warlocks365/gpu-lan-runtime（公开仓库）
- **审计基线**：commit `675985a`（"Add Simplified-Chinese docs…"，仓库唯一提交）
- **审计方式**：`--depth 1` 克隆到临时目录 → 逐文件通读（Python / PowerShell / bash）+ 敏感模式全仓扫描 + git 历史检查
- **结论**：**P2 安全，无 P0/P1 风险，允许安装**

---

## 一、结论速览

| 维度 | 结果 |
|---|---|
| 恶意/可疑代码 | 无 |
| 凭据泄露 | 无（零真实 secret；`gpu.json` 已被 .gitignore 排除且从未入库） |
| 网络外联 | 仅用户配置的 SSH host；无第三方域名、无回传电话 |
| 下载源 | 仅官方源（astral-sh GitHub release + download.pytorch.org） |
| 破坏性操作 | 服务端脚本均带备份 + 幂等标记块 + 防锁定检查 |
| 许可 | Apache-2.0（宽松，可自由复用/商用） |
| 审计判定 | **P2（安全）** |

---

## 二、逐文件发现

### 客户端 `client/gpu.py`（866 行）
- 网络面干净：唯一外联是 `sock.connect((host, port))` 与 paramiko `connect/exec_command/open_sftp`，目标**全部来自用户 gpu.json 配置**。无 `http(s)://`、无 urlopen/requests、无任何第三方上报。
- 凭据面干净：公钥路径 `key_filename=`；密码仅在 `--bootstrap` 用 `getpass` 交互输入（不落盘、不进参数列表）；`allow_agent=False, look_for_keys=False`（不误用本机 ssh-agent 其它身份）。
- 命令构造正确：Windows 用 `subprocess.list2cmdline`、POSIX 用 `shlex.join`（无引号丢失）；环境变量用带引号的 `set "VAR=value"`（规避 cmd 贪婪 set 把尾随空格吸进值里的致命 bug）。
- 退出码纪律完整：0 成功 / 1 代码 bug / 2 环境问题 / 3 用法错 / 4 本地配置不完整；`--doctor` 先行、能区分 2 与 4。
- 参数透传正确：`argparse.REMAINDER` + 手工处理 `-h/--help`，远程命令可带自己的 `--help`。
- 配置健全性检查：host/user 空值或 `REPLACE_WITH_*` 占位符会直接拒绝执行。

### 客户端配置 `client/gpu.json.template`
- 纯默认值 + 说明注释；host/user 均为 `REPLACE_WITH_*` 占位符。默认 `exclude` 已排除 `.env`、数据集、权重与密钥文件类型，同步上行的内容面被刻意收窄。

### 服务端 `server/ssh_server_windows.ps1` / `ssh_server_linux.sh`
- 写 `sshd_config` 前先 `Copy-Item`/`cp` 备份（带时间戳）；用 `# --- gpu remote executor ---` 标记块实现幂等改写（重复跑不叠加）。
- 指令块**插入到首个 `Match` 块之前**——规避 Windows 出厂 `Match Group administrators` 尾块把后续指令全部吞进 admin 作用域的坑。
- 新建账户随即移出 Administrators 组（确认非管理员）；一次性随机密码（20 位字母数字）仅控制台打印一次、**不写日志**。
- Windows：icacls 关闭继承并最小授权 `.ssh`/`authorized_keys`（sshd 对他人可写的 key 文件静默忽略）；防火墙规则限定 `-RemoteAddress $LanCidr`（默认 192.168.1.0/24，注释建议收窄到 /32）。
- Linux：`chpasswd` 一次性密码、`Match` 前插入、防火墙按 CIDR。

### 服务端 `server/finalize_sshd_windows.ps1` / `ssh_finalize_linux.sh`（部署收尾）
- 关密码登录前做**防锁定检查**（authorized_keys 为空/不存在则中止并报警），不会把自己锁在门外；同样带备份与幂等标记块。

### 服务端 `server/install-runtime.py`（B 机隔离运行时安装）
- `tarfile.extractall(filter="data")`（3.12+ 安全过滤器，**防 tar 路径穿越**，无 `filter=None` 裸提取）。
- 下载源仅两处官方 https：`api.github.com/repos/astral-sh/python-build-standalone` 与 `download.pytorch.org`。
- 全程本机隔离 venv，不碰系统 Python。

### 服务端 `server/sitecustomize.py`（显存配额钩子）
- 仅当环境变量 `GPU_MEM_FRACTION` 存在时才动作；提示输出全部走 **stderr**（已修复旧版 stdout 污染问题）。

### 文档与许可
- README / AGENTS / docs 中英双语齐全；`docs/agent-interface.md` 是给 AI agent 的调用契约，`docs/platforms.md` 记录 Windows/POSIX 平台坑位（Match 块、KbdInteractive、UsePAM 复活密码通道等）。
- LICENSE = Apache-2.0。

---

## 三、敏感模式全仓扫描

- 正则覆盖：API key / secret / password / token / 各类私钥头（`BEGIN … PRIVATE KEY`）/ AWS `AKIA*` / GitHub `ghp_*` / OpenAI `sk-*` / Slack `xox*` / Google `AIza*`。
- 命中项全部为**文档对密码认证机制的合法描述**（bootstrap 唯一密码步骤、finalize 关密码通道等）与代码中 `getpass` 交互流程；**无任何真实凭据**。

## 四、Git 历史检查

- 仓库仅 1 个提交（`675985a`），无历史包袱。
- 全历史从未包含 `gpu.json`（含真实 IP/账号的配置文件被 .gitignore 排除，模板里的占位符可放心公开）。
- 无二进制大文件、无子模块。

## 五、低危观察项（不阻断安装）

1. **`AutoAddPolicy()`（TOFU 主机密钥信任）**：paramiko 首次连接静默接受任意 host key。缓解：防火墙限定 LAN CIDR + 公钥认证 + 配置文件固定 host。对 LAN 便捷工具是可接受的取舍；如需更强可在未来升级为 `known_hosts` 校验。
2. **PowerShell 脚本 `$ErrorActionPreference='Continue'`**：步骤出错记日志继续跑（尽力而为设计），不会中途硬停。运维上需在部署后回看 `_sshd_windows.log`。
3. **一次性随机密码为 20 位字母数字**（无符号）：熵约 119 bit，足够一次性引导使用。

---

## 六、安装动作记录（2026-09-03，用户确认"两种都做"）

| 位置 | 内容 | 用途 |
|---|---|---|
| `C:\Users\warlocks\.workbuddy\skills\gpu-lan-runtime\` | 完整仓库克隆（含 .git，origin 指向 GitHub）+ 根目录新增本地 `SKILL.md` 包装 | **WorkBuddy 技能**：GPU 远程任务场景自动发现加载；SKILL.md 指引按序读仓库内 AGENTS/agent-interface/platforms 权威文档 |
| `C:\Users\warlocks\Tools\gpu-lan-runtime\` | 完整仓库克隆（独立工具副本） | **日常直接调用主副本**：`python client/gpu.py …`；与旧 `D:\gpu-remote\gpu.py` 同源但已含跨平台 + 自动探测等新特性 |
| （原临时审计目录 `/tmp/gpu-lan-runtime-scan`） | 审计用克隆 | 可删 |

- SKILL.md 为本地新增、**未提交上游**——保持 GitHub 仓库纯净（它是对所有使用者通用的开源项目），且克隆 `git pull` 更新不受未跟踪文件影响。
- 两处副本更新方式一致：`git pull`。

## 七、遗留事项

1. **B 机（192.168.1.41）部署收尾待真人执行**：管理员跑 `finalize_sshd_windows.ps1` 关闭密码通道；随后把新版 `server/sitecustomize.py`（stderr 版）覆盖 B 机共享 venv 里的旧版（旧版 stdout 污染待修项，仓库版已修复，直接取用即可）。
2. **SKILL.md 是否推送上游（可选）**：若希望其他 WorkBuddy/Claude Code 用户 clone 即得技能入口，可将 SKILL.md 提交并推送；届时需同步给本报告留档。
3. 旧副本 `D:\gpu-remote\gpu.py` 与新仓库版的关系：新仓库版为演进后正式版，旧副本可按需退役，避免双份漂移。
