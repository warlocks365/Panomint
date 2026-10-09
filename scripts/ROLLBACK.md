# 回滚预案（发布安全 · Silver 证据）

> 对应生产就绪记分卡「发布安全」维度：Bronze（能回退）→ Silver（渐进发布 + 回滚预案**演练**）。
> 本文不是「写着好看」的计划书，而是 `scripts/rollback.sh` 的操作手册。
> 文末记录了 2026-10-10 真实演练的结果与演练中暴露的两个生产缺陷。

## 一句话

出问题时：`bash scripts/rollback.sh list` 选基线 → `bash scripts/rollback.sh to <名称>` → 自动验证。

---

## 0. 三十秒速查

```bash
cd /home/warlocks/pano-album

# 线上现在是什么版本 / 上次部署与回滚结果 / 健康状况 / 有哪些退路
bash scripts/rollback.sh --status

# 执行回滚
bash scripts/rollback.sh to <基线名称>

# 只校验当前状态是否与某基线一致（不改动任何东西）
bash scripts/rollback.sh verify <基线名称>
```

`--status` 是只读的，出事时**第一条命令就跑它**，它会告诉你：
当前各服务的镜像 ID、上次部署成败、上次回滚成败、健康探针、以及可用的退路列表。

---

## 1. 什么算「需要回滚」

| 现象 | 判断依据 | 动作 |
|---|---|---|
| 部署后 `/ready` 不恢复 | `curl -sS http://127.0.0.1:8088/ready` 非 `{"status":"ready",...}` | 回滚 |
| 业务端点 5xx 增多 | `docker compose logs api \| grep -c 'status": 5'` | 回滚 |
| 容器反复重启 | `RestartCount > 0` | 先查日志，多为配置/迁移问题 |
| 巡检报「上次部署失败」 | `cat /home/warlocks/.pano-ops/last_deploy.txt` | 修好再部署，不是回滚 |
| **巡检报「同一 tag 供应多个镜像」** | 滚动升级只重建了一半 | 补齐重建，**不要**回滚 |
| **巡检报「上次回滚失败」** | `cat /home/warlocks/.pano-ops/last_rollback.txt` | **最高优先级**，人可能误以为已退回 |

最后两条容易误判成「需要回滚」，但处置完全不同 —— 先看巡检报的是什么，再动手。

---

## 2. 三个核心概念

### 2.1 基线（baseline）—— 用镜像 ID，不用 tag

基线记录的是**每个服务当前实际在跑的镜像 ID**：

```
SERVICE              TAG                          IMAGE_ID
api                  panomint-app:latest          sha256:982b5fc9c03e…
embed-worker         panomint-app:latest          sha256:982b5fc9c03e…
```

**为什么不按 tag 回滚**：tag 是可变的。`panomint-app:latest` 下一次构建就指向别的东西，
按 tag 回滚等于「回滚到当前版本」—— 命令显示成功，实际一步没退。
演练中实测过：造坏版本后 tag 指向坏镜像，此时按 tag 回滚毫无作用。

### 2.2 共享 tag 的服务必须整组回滚

`api` 与 `embed-worker` / `tag-worker` / `phash-worker` / `faces-worker`
在 compose 里写的是**同一个 tag** `panomint-app:latest`。所以：

- 只回滚其中一部分 → 立刻产生「同 tag 两个镜像」的版本错配；
- 现象是 api 用新逻辑、worker 用旧逻辑，行为不一致且极难归因；
- 巡检会把这种情况报成 P0（`同一 tag 供应多个镜像`）。

`rollback.sh` 已把这 5 个服务固化为 `SHARED_TAG_SERVICES`，**回滚时整组一起动**。

### 2.3 回滚不碰数据

只重建容器，**不执行 `docker compose down`（更不会 `down -v`）、不 prune 卷、不删任何镜像**。

边界：如果某次升级**同时改了数据库迁移**，单纯回滚镜像并不能回退 schema。
本脚本不会自动处理这种情况（自动 down-migration 的风险远大于收益），
遇到「版本回退涉及 schema 变更」时必须人工判断，必要时用 `scripts/restore.sh` 从备份恢复。

---

## 3. 标准操作流程

### 3.1 部署前：先留退路

**这一步是硬要求。** 没有基线就没有退路，出事时才发现就晚了。

```bash
bash scripts/rollback.sh record pre-deploy-$(date +%Y%m%d-%H%M%S)
```

记录内容包括：每个服务的 tag、实际镜像 ID、以及当时的健康快照与全部镜像清单。
基线保存在 `/home/warlocks/.pano-ops/rollback/<名称>/`，**不在仓库里**（服务器本地状态）。

### 3.2 部署

```bash
bash scripts/deploy.sh api                       # 失败会返回非 0 并立即告警
bash scripts/deploy.sh --accept api              # 成功后顺便登记指纹基线
```

### 3.3 出事了：回滚

```bash
bash scripts/rollback.sh list                    # 1. 看有哪些退路
bash scripts/rollback.sh show pre-deploy-20261010-0030   # 2. 看某个退路详情
bash scripts/rollback.sh to pre-deploy-20261010-0030    # 3. 执行回滚
```

`to` 会自动完成 5 件事，任一步失败即判失败：

1. 校验基线里的镜像**还在**（镜像被清理了会明确报错，不会闷头往下走）
2. 把基线的镜像 ID 重新打回各服务的 tag
3. `docker compose up -d --force-recreate` 整组重建（必须 `--force-recreate`，
   否则镜像 ID 变了但容器仍指向旧 ID，docker 认为「配置没变」而不重建 —— 这正是第②项查出的故障成因）
4. 重建反代层（`web` / `caddy`），让 nginx 重新解析 api 的新 IP（**见下方已知缺陷 1**）
5. 等 `/ready` 恢复，并逐服务比对镜像 ID、校验容器数与卷数

### 3.4 回滚后确认

```bash
bash scripts/rollback.sh verify <基线名称>       # 逐项比对，不一致会列出 [P0]
curl -sS http://127.0.0.1:8088/ready             # 应为 {"checks":{...all ok},"status":"ready"}
docker compose ps                                 # 应为 11 个 running
docker volume ls -q | wc -l                      # 应为 43
```

**「命令返回 0」不等于「回滚成功」。** 必须以 `verify` 与上面三条实测输出为准。
`verify` 会检查 `/ready` 的**三个子项**（disk/postgres/valkey），
只看 HTTP 200 会漏掉「依赖降级但仍返回 200」的情况。

---

## 4. 回滚失败怎么办

`rollback.sh` 在任何一步失败时都会：写 `last_rollback.txt`（`status=failed`）+ 立即告警 +
巡检在后续每轮继续报 P0。

**回滚失败是最需要人工介入的状态** —— 人以为退回去了，实际还停在故障版本上。

```bash
cat /home/warlocks/.pano-ops/last_rollback.txt   # 失败原因
bash scripts/rollback.sh list                    # 换另一个基线
bash scripts/rollback.sh --status                # 确认线上实际版本
```

如果**所有基线都不可用**（镜像都被清理了），退路是：

1. `bash scripts/deploy.sh <服务>` —— 用当前源码重新构建一个已知良好版本
   （这也是为什么**源码必须在版本控制下**，部署目录不是 git 仓库这点很致命）
2. 必要时 `scripts/restore.sh` 从备份恢复数据（详见 `scripts/RESTORE.md`）

---

## 5. 已知缺陷与限制（演练实测，非推测）

### 缺陷 1：nginx 上游解析过期导致「假 502」

**现象**：api 容器被重建后拿到新 IP（如 `172.19.0.8`），
但 `web`(nginx) 容器没动，它在启动时解析过一次 `api` 域名并缓存了旧 IP。
结果：

- `web` 容器内直连 `api:8080` → **正常**（`{"status":"ok"}`）
- 经 nginx 的 `/ready` / `/health` → **一律 502**
- `nginx error.log` → **空的**（连的是早已不存在的旧 IP，没有留下有效线索）

**为什么危险**：api 明明是好的，`/ready` 却报 502，排查极易被引向 api 容器本身。
2026-10-10 的演练里就因此**一度判成「回滚失败」**，实际只是反代层需要重载解析。

**已修**：`rollback.sh` 与 `deploy.sh` 在重建 api/worker 之后，都会一并
`docker compose restart web caddy`。手工处置：

```bash
cd /home/warlocks/pano-album && docker compose restart web caddy
```

### 缺陷 2：`docker compose config --services` 偶发返回空

**现象**：该命令偶发返回空输出（实测同一命令连跑 5 次都正常，但在连续构建后的某些时刻会空一次）。

**为什么危险**：原本的服务名校验写成

```bash
docker compose config --services | grep -qx "$svc" || 判为「拼错了」
```

于是一次空返回就把**合法部署拦下来**，退出码 2 —— 而这恰好发生在**最需要部署成功的时刻**
（紧急回滚被挡住）。演练中实测踩到过。

**已修**：`svc_known()` 先判断命令有没有成功拿到名单，拿到但名单里没有才算拼错；
拿不到就重试 3 次，仍失败则**放行并告警**。宁可部署一次未知服务，也不能让探测命令的抖动挡住回滚。

### 限制 1：不能自动回退数据库 schema

见 §2.3。涉及 schema 变更的版本回退必须人工判断。

### 限制 2：部署目录不是 git 仓库

`/home/warlocks/pano-album` 实测 `fatal: 不是 git 仓库`。
所以「回到某个历史版本」只能靠**已构建的镜像**，不能靠 checkout 源码重新构建。
这直接决定了：**镜像就是唯一的退路，绝不能随手清理。**

### 限制 3：基线只存在于本机

`/home/warlocks/.pano-ops/rollback/` 不在仓库里。换机器/重建后基线全丢。
需要长期保留的基线，应把镜像导出成 tar 存到备份盘：

```bash
docker save -o /home/backups/pano/panomint-app-982b5fc9.tar sha256:982b5fc9c03e…
```

---

## 6. 演练记录（2026-10-10，真实执行）

命令：`bash scripts/rollback.sh drill`

| 步骤 | 动作 | 真实结果 |
|---|---|---|
| 1 | 记录基线 | `drill-20261010-003608`，8 个服务的镜像 ID 全部固化 |
| 2 | 制造坏版本 | 注入语法错误的 `.go` 文件（**未修改任何 Dockerfile**） |
| 3 | 部署坏版本 | `deploy.sh` 退出码 **17**；`internal/media/zz_rollback_drill_probe.go:6:7: syntax error: unexpected name is at end of statement` |
| 4 | 巡检查觉 | P0「上次部署失败且未恢复（原因=build_exit=17）」 |
| 5 | 清理探针 | 删除成功 |
| 6 | 执行回滚 | 首轮**失败**（暴露缺陷 1），修复后重跑通过：8 个服务全部回到基线镜像 |
| 7 | 回滚验证 | 镜像逐个 `[ok]`、`/ready` 三项全 ok、11 容器、43 卷 |
| 8 | 回滚失败检测 | 用指向不存在镜像的基线执行 `to` → 退出码 **1**、`status=failed`、立即告警；巡检下一轮报 P0「上次回滚失败 → 可能仍停在故障版本，且人工可能误以为已退回」 |

演练期间发现的两个生产缺陷（缺陷 1、缺陷 2）均已修复并复测通过。
**它们都是只有真跑一遍演练才会暴露的** —— 写在文档里的预案不会自己发现问题。

演练后线上状态已确认恢复：11 容器 running、43 卷不变、`/ready` 全 ok、
坏版本镜像仍保留（`f120c01d2e4a`，退路未被删掉）。