# 四文档审查修正总览 (overview)


# 四份文档全面审查与修正总览

> 日期：2026-08-24\ 审查团队：产品战略团队（主理人方向明 + 竞析/数析/析客/路径）\ 审查对象：PRD v3.0 / TDD v1.0 / DDL v1.0 / API v1.0\ 修正后版本：PRD v3.1 / TDD v1.1 / DDL v1.1 / API v1.1


---


## 修正总览

| 文档 | 修正前 | 修正后 | 主要修正项数 |
| --- | --- | --- | --- |
| PRD | v3.0 | v3.1 | 12 |
| TDD | v1.0 | v1.1 | 10 |
| DDL | v1.0 | v1.1 | 16 |
| API | v1.0 | v1.1 | 10 |


---


## 关键修正摘要


### 1. 许可商业化合规（PRD §12.2 完整替换）




- 行动清单按 P0/P1/P2 优先级排列。


### 2. DDL 技术修正（16项）

- 补 PostGIS 扩展 + `geometry(Point, 4326)` 替代 `POINT`

- media 表补全：deleted\_at(回收站)、相机元数据(8字段)、视频元数据(5字段)、rating、live\_photo\_pair\_id、video\_preview\_at

- 补 transcode\_jobs.node\_id / index\_jobs.user\_id / share\_links.access\_count / albums.description / tags.color

- 新增 album\_comments 表 / updated\_at 触发器 / 回收站+评级+实况照片索引


### 3. API 端点补全（10项）

- POST /media/upload（分块续传）/ GET /media/:id/download / POST /media/:id/rate

- GET /albums/:id / GET+POST+DELETE /albums/:id/comments

- HLS 分享鉴权（?key= 参数方案解决 hls.js 不支持自定义 header）

- POST /s/:token/bandwidth-test（访客无JWT带宽自测）

- §16 WebDAV（7个方法端点）


### 4. TDD 运维补全（10项）

- §4.2 扩充 360 引擎详细设计（WebGL shader/iOS权限/WebXR生命周期/hls.js错误恢复/移动端优化）

- §6.1 新增算力节点 agent 协议设计

- 新增 §8 运维与可观测性（监控告警/日志/备份恢复/CI-CD/健康检查/配置密钥/CDN/数据迁移）


### 5. PRD 功能补全（12项）

- §6.10 查看器补 EXIF/评级/实况照片；§6.4 补评论；§7 API表补9端点

- §8 数据模型同步 DDL 全字段；§12.2 完整许可矩阵

- §11.2 数据迁移方案；§11.3 技术债务风险；§3 NFR 补可观测性


---


## 交付物清单

1. `相册系统详细需求文档.md`（PRD v3.1）

1. `技术设计文档.md`（TDD v1.1）

1. `数据库DDL.md`（DDL v1.1）

1. `API详细契约.md`（API v1.1）

1. `审查修正清单.md`（主理人预编清单，含完整许可矩阵）

1. `overview.md`（本文件）

