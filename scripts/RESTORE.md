# 全景相册恢复手册（Phase 6 T6.3）

配套 `scripts/backup.sh` 与 `scripts/restore.sh`。**人工执行**，脚本已内建非破坏性保护。

> 红线：恢复全程**绝不** `docker compose down` / `down -v` / 卷 prune / 删除任何现有卷。
> 所有还原动作一律写入**新库 / 新卷**，原库原卷保持不动。

---

## 一、为什么必须"DB + 文件"一起救

历史事故：`docker compose down -v` 重建了 `mediadata` 卷 → **媒体文件实体灭失**，
而 DB 中 113 条 `media` 记录仍在 → 记录成为**孤儿**（有记录、无文件）。
所以恢复**同时**包含 DB 与文件两个部分，最后必须做**对账**。

| 备份内容 | 覆盖 | 备注 |
|---|---|---|
| `pano_album.dump` | PG 全部数据（`pg_dump -Fc`） | 一致性快照 |
| `schema.sql` | 表结构 DDL | 永久参考 |
| `mediadata.tar.gz` | 媒体原文件 + 缩略图 + HLS | 红线要求 |
| `config.tar.gz` | `.env` / `docker-compose.yml` / `docker/`（含 caddy 证书） | 含密钥，权限 600 |
| `SHA256SUMS` | 上述文件的校验和 | 恢复前先校验 |

---

## 二、6 步恢复流程

### 第 1 步 · 停写入（只 `stop`）
```bash
cd /home/warlocks/pano-album
docker compose stop api index-worker transcode-worker embed-worker
# db / valkey 保持运行，供后续 psql / pg_restore 使用
```

### 第 2 步 · 校验备份可用（不碰数据）
```bash
scripts/restore.sh verify /home/backups/pano/<时间戳>
```

### 第 3 步 · 救 DB（还原到**新库**）
```bash
scripts/restore.sh db /home/backups/pano/<时间戳>
# → 产出新库 pano_album_restore_<时间戳>，原库 pano_album 不动
```
确认无误后正式切换：把 `.env` 的 `POSTGRES_DB` 指向新库 → `docker compose up -d db`。
（更稳妥的做法：保留原 `pgdata` 卷不删，仅改库名指向。）

### 第 4 步 · 救文件（解到**新卷**）
```bash
scripts/restore.sh media /home/backups/pano/<时间戳>
# → 产出新卷 pano-album_mediadata_restore_<时间戳>，原卷不动
```
确认无误后，把 `docker-compose.yml` 中 `mediadata` 的卷名指向新卷，再 `docker compose up -d`。

### 第 5 步 · 对账（以 DB 的 `media.path` / `hash` 为准）
```bash
scripts/restore.sh reconcile
```
- **A. DB 有记录、磁盘无文件（孤儿记录）** → 从备份 media 卷回填该文件；若文件确已永久丢失，再清理孤儿记录。
- **B. 磁盘有文件、DB 无记录（未入库）** → 重新入库：
  ```bash
  docker compose run --rm --no-deps --entrypoint /usr/local/bin/indexctl api \
    scan -dir /data/media     # ⚠️ 必须对**媒体根**扫描：media.path 存的是相对扫描根的路径
  ```
- 缩略图与 HLS **无需从备份回填**，可由队列/worker 重跑：
  ```bash
  docker compose run --rm --no-deps --entrypoint /usr/local/bin/embedgen api -mode encode
  ```
  （增量向量化 `embed-worker` 也会自动清扫补算）

### 第 6 步 · 起服务并验证
```bash
docker compose up -d
docker compose restart web     # ⚠️ 必做：nginx 启动时解析 upstream，不跟随 api 重建 → 否则 502
curl -sS http://127.0.0.1:8088/ready   # 期望 {"status":"ready",...}
```

---

## 三、季度演练建议

每季度一次：把一份备份还原到**临时库 + 临时卷**，抽查 10 条 `media` 文件可达，
随后删除**本次新建的**临时库/临时卷（只删临时对象，绝不动生产对象）。

---

## 四、相关文件

| 文件 | 用途 |
|---|---|
| `scripts/backup.sh` | 产生备份（cron 每日） |
| `scripts/restore.sh` | `list` / `verify` / `db` / `media` / `reconcile` |
| `scripts/pull-backup.sh` | 开发机侧，每周把最新备份拉回本机归档 |
| `scripts/ops_check.sh` | 定时巡检 + 邮件告警 |
| `/home/backups/pano/LAST_SUCCESS` | 最近一次成功时间戳（供巡检判定新鲜度） |
| `/home/backups/pano/backup.log` | 备份流水日志 |
