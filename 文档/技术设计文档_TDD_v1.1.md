---
title: 技术设计文档 (TDD v1.1)
---

<Heading id="tIh7PUSyxE41XNvSslheRc" level="1">
  全景相册系统 · 技术设计文档（TDD v1.0）
</Heading>

<BlockQuote id="DWS8aUUZHaJ2SJQbGqxkzk">
  <Paragraph id="jZoPxE3SW0Qn3mr0gcLjiG">
    版本：v1.1 ｜ 状态：草案 ｜ 日期：2026-08-24\
    配套：PRD v3.1（群晖 Synology Photos 优先 / PhotoPrism 补位 / 360 视频自研）\
    范围：系统架构、服务模块、核心流程、360 播放引擎、安全、伸缩、运维可观测性、技术选型\
    关键决策：账户与权限采用<Mark bold>独立后台管理模式</Mark>（不对接 DSM）
  </Paragraph>
</BlockQuote>

<Divider id="iPbIEjSTfC3l3YjAw5cs2X" />

<Heading id="IcSqjrWuD1rkwgVqlGsyxz" level="2">
  1. 概述与追溯
</Heading>

<Paragraph id="cLES3hihMNpmWJYDokkiNy">
  本系统为 <Mark bold>100% 自研</Mark>的自托管相册系统，目标是作为 <Mark bold>Synology Photos 的增强替代品</Mark>，并补齐群晖缺失的 <Mark bold>360° 全景视频交互播放（陀螺仪/VR 头追/自适应码率/微信 H5 分享）</Mark>。
</Paragraph>

<Table id="ddTUz8mowLJVgbhsrArZGx" readonly rowHeader>
  <TableRow id="LEICk5Yr8RoszeClMyuLLS">
    <TableCell id="rwdxC0kK5L5gTEZpwCO5a2">
      <Paragraph id="BukIiscOIs3YOGpfhOqUDV">
        需求文档章节
      </Paragraph>
    </TableCell>

    <TableCell id="QM9EeyskxHkLsxvclwb7aQ">
      <Paragraph id="qIdUxzqv3LXpcBREKSC8bl">
        本设计对应
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="M5GsAI0LdyfRhd8JRKxFo1">
    <TableCell id="kkmE6FR3nv0muA7pZlGc4l">
      <Paragraph id="yREHQc7GE2PnicyAyvyBer">
        §1 产品概述 / §1.4 对照表
      </Paragraph>
    </TableCell>

    <TableCell id="5l93R4O8446OEx7VOg1fTy">
      <Paragraph id="F84HftRf7dFjfy7t7TkcWv">
        §2 架构、§3 模块
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="xWJgmyEn0sXpwn1xae7wPG">
    <TableCell id="Ojv7vkMej3TEhbEmGFptND">
      <Paragraph id="UDcAnVnUotC0frOXgw4m7Q">
        §4 总体架构
      </Paragraph>
    </TableCell>

    <TableCell id="BH9myVwNLJu9n1Tmh7xvU0">
      <Paragraph id="Etsu3tGyKQV2qyFruFziD3">
        §2 架构、§6 算力分离
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="DZal715ztyyRx8l8v0Upox">
    <TableCell id="qBMUL5Bw8YvHWN3dGK6uvj">
      <Paragraph id="bMnAPz7OZZN3hoNXKVkhsz">
        §5 模块总览 P0–P2
      </Paragraph>
    </TableCell>

    <TableCell id="23KAqbmqytSZmVcbOcpDGv">
      <Paragraph id="msBZrhbynL0tSuy6oHEOEq">
        §5 里程碑
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="G35CKOWOE5HBBOJknVenMi">
    <TableCell id="jY9LOb5H9yMi6CwGjUfXNT">
      <Paragraph id="99pRSyH3TRNncjT8eUaOLA">
        §6 前端页面
      </Paragraph>
    </TableCell>

    <TableCell id="d6lqhrDbLG9tctxWfGqoDR">
      <Paragraph id="4SjLJCvTLwIQD7W3viaDCG">
        §4 核心流程（前端调用后端）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="2zeLV4k7KUsU71VihQeAl1">
    <TableCell id="FqoVlySHC6R9y8cZsmc2WF">
      <Paragraph id="ML5jY4ulcf8nFdtXrXQrvU">
        §7 API 契约摘要
      </Paragraph>
    </TableCell>

    <TableCell id="5Nl9fntqBDw7X2kgQGpe3c">
      <Paragraph id="deF1wHCwaqO2innqj34Gcv">
        《API 详细契约.md》
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="mUbjdoa3kjlt1jx2SDBnP0">
    <TableCell id="pT1ZdBEVKIqbgJ0T2X3Jhr">
      <Paragraph id="FxMnhNSt94fe4MsNtePeke">
        §8 数据模型
      </Paragraph>
    </TableCell>

    <TableCell id="AGwYEmhTmxqTVUGOfauuNl">
      <Paragraph id="MvxAjp7AZTYwm6gzlkfyB9">
        《数据库 DDL.md》
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="s98LgeRIoE7D79VyhihXyp">
    <TableCell id="U5gPJG0Jb0Weq3w6bXW7zi">
      <Paragraph id="m0wE0dVTzeZXuEGDNpkkxt">
        §11 风险
      </Paragraph>
    </TableCell>

    <TableCell id="HtOiSQDD5cUJFgw6hBWJEa">
      <Paragraph id="tXlG67n1PcvYcMZdvX8fUo">
        §9 风险与对策
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Paragraph id="cOI7UFeO2sEdpg0syfMtxk">
  <Mark bold>设计原则</Mark>：存储与算力分离；成熟库拼装 + 仅 360 引擎自研；一切外部访问 HTTPS；账户/权限独立后台。
</Paragraph>

<Divider id="91fD3LgPHCXOqDazOr6kbH" />

<Heading id="AT0Kmlzrv2fCkjDvkVrJBe" level="2">
  2. 系统架构
</Heading>

<Heading id="Kekf4JWZSq8L4pQDOGH6PF" level="3">
  2.1 部署拓扑（ASCII）
</Heading>

<Code id="PEAy742JDC7Rl4EHv98kdi">
  ```
                           ┌─────────────────────────────────────┐
     手机/平板/PAD         │            公网 (HTTPS)              │
     微信内置浏览器  ──────▶│   Caddy 反代 + WAF + 自动证书        │
     VR 头显  ───────────▶│   (WebXR/DeviceOrientation 强制 HTTPS)│
     浏览器 / PWA  ───────▶│        /api/*   /s/<token>(H5)        │
                           └───────────────┬─────────────────────┘
                                           │
                      ┌────────────────────┼────────────────────┐
                      ▼                    ▼                    ▼
              ┌──────────────┐   ┌──────────────┐    ┌──────────────┐
              │ API 网关/媒体 │   │  AI 推理服务  │    │ 转码管线节点  │
              │  (Go)        │   │ (FastAPI)    │    │ (ffmpeg+GPU) │
              │ 鉴权/RBAC     │   │ 人脸/标签/地图│    │ 缩略图/HLS    │
              │ 媒体库/相册    │   └──────┬───────┘    └──────┬───────┘
              │ 360 播放WebXR │          │ pgvector          │
              └──────┬───────┘          │                   │
                     │                  ▼                   │
              ┌──────┴───────┐   ┌──────────────┐   ┌──────┴───────┐
              │ PostgreSQL   │   │ 对象存储/NAS  │   │ 消息队列      │
              │ + pgvector   │   │(MinIO/S3)¹   │   │(BullMQ/Valkey)²│
              │ 元数据/向量   │   │ 原文件+缩略图 │   │ 索引/转码任务 │
              └──────────────┘   └──────────────┘   └──────────────┘
                     ▲                                         
                     │                                         
              ┌──────┴───────┐                                 
              │ DS1819+ NAS   │  仅做文件系统/对象存储（Atom C3538 无 GPU）
              │ (存储卷)      │  不参与 AI/转码
              └──────────────┘
  ```
</Code>

<BlockQuote id="gFIh2d7AZ8J2dF8oUWgFlL">
  <Paragraph id="gVavLYsiF0EGZLF9dxtnS6">
    ¹ <Mark bold>MinIO</Mark>（AGPL v3）作为独立 S3 兼容服务通过 API 调用，不构成对应用代码的 copyleft 传染（不修改/不分发）。若需完全规避 AGPL，可替换为 <Mark bold>SeaweedFS</Mark>（Apache 2.0）。\
    ² <Mark bold>Valkey</Mark>（BSD-3-Clause）是 Redis 的 Linux 基金会 fork，用于替代 Redis（2024 起改 RSALv2/SSPL，非 OSI 认证开源），BullMQ 完全兼容。
  </Paragraph>
</BlockQuote>

<Heading id="xO0muaVkfAPWZecNSma9T2" level="3">
  2.2 分层职责
</Heading>

<Table id="YJan6slE7lN8JPBDEnYVNE" readonly rowHeader>
  <TableRow id="pek936jdrH2yRjuxprFphM">
    <TableCell id="R16V2vWQLx3jITsSRgcbg0">
      <Paragraph id="bn5Qj6TbsjxjfqwCXgXHGS">
        层
      </Paragraph>
    </TableCell>

    <TableCell id="1uPTbqO7A21JvHja4w8Bek">
      <Paragraph id="N68hDknsBU3qDzKaTI1IpU">
        组件
      </Paragraph>
    </TableCell>

    <TableCell id="ywQPDOa5Lp9CLVbk1GrxZ0">
      <Paragraph id="pc60vUCaOVltChNixaSvhV">
        职责
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="6luq2jxLiXYuVL8Y6e4vDs">
    <TableCell id="TRUBRAJ69GWFmwrgPfSDPO">
      <Paragraph id="5xw5G5BNUwIbiSDLPIRgYl">
        客户端
      </Paragraph>
    </TableCell>

    <TableCell id="lK6DlM0qIJJrpBsxRBRkkX">
      <Paragraph id="KfZrRfjAlMQ3YktTWfyU7i">
        Web/PWA、移动端 PWA、VR 头显、微信 H5
      </Paragraph>
    </TableCell>

    <TableCell id="n2kLx5xEwMRmPSf4xOEgt9">
      <Paragraph id="dcu3jzpzn7I14bXc8KFEMT">
        浏览/上传/360 交互/分享
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="srkloaRXGdecZxiqYxh0bP">
    <TableCell id="BC9qxNwIOGNgrpiVlkwwRl">
      <Paragraph id="tisH9jUkEEc8NqhPz3qR3f">
        边缘
      </Paragraph>
    </TableCell>

    <TableCell id="UkwcAR2Y9niDRdh2OTJwYz">
      <Paragraph id="CHoqZBTM6J2UvQVHx15LXr">
        Caddy 反代 + CDN
      </Paragraph>
    </TableCell>

    <TableCell id="NHIneHzZyjBCONlv6DFqFX">
      <Paragraph id="XWDZupXuzvvvpJ2aVkEp9O">
        HTTPS、ABR 边缘缓存、WAF、限流
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="U7DIMxcdkfKltxJlVjjk24">
    <TableCell id="1uaxcAxn7TmdK4QPH3x94n">
      <Paragraph id="mDOhId42Wthq4gLoMpjxM6">
        应用
      </Paragraph>
    </TableCell>

    <TableCell id="CS4JdVuSBb5lBlPlyYivFc">
      <Paragraph id="uapBv3nCzWuB5Cogt5S6VJ">
        API 网关（Go）
      </Paragraph>
    </TableCell>

    <TableCell id="HUWljQ64PFByj4oU2fRkRd">
      <Paragraph id="Z8AnEvhQuViVFaxF0d2cy6">
        鉴权/RBAC、媒体库、相册、人物、地点、分享、360 播放页渲染
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="bbexN02iyQMhSjGmfNOjfA">
    <TableCell id="MrZrVOjQ4JtJx7k943kQFP">
      <Paragraph id="S5WWghZzMqiPLTuvRaTSyJ">
        地图/地理
      </Paragraph>
    </TableCell>

    <TableCell id="nHj7mprotyQEiVmb7CTZxn">
      <Paragraph id="qXXbQu3Bv2QTbZ3XYFJfhA">
        API 网关（Go）+ 瓦片代理
      </Paragraph>
    </TableCell>

    <TableCell id="jBH9ye9uJlLP3W7nym9JTv">
      <Paragraph id="w4OMjfv31MPOVHQ7vfxtG5">
        空间聚合(supercluster)、时间轴直方图、模糊筛选、地理搜索、中外国界切换、GCJ-02 转换
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="xnrdWv6CoNx2yVOUT6r0Cv">
    <TableCell id="btoy3b54eLyUPqUh9dCXO1">
      <Paragraph id="43QF2Tlg4je0ebqb9rNYPb">
        AI
      </Paragraph>
    </TableCell>

    <TableCell id="gwhKpVBIL4z5wcyrUxSAZ3">
      <Paragraph id="o9wGdN6aZAZYLVmjtxof10">
        FastAPI 微服务
      </Paragraph>
    </TableCell>

    <TableCell id="eFbRs393XEhCwwGgEjQ7Zf">
      <Paragraph id="VPTDjfEtzCAbzjEuhKNxEE">
        人脸检测/聚类、自动标签、地理编码、Clean Up
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="PbTCUlLs1LdjqGZcuyX18I">
    <TableCell id="WKRNAuxPp4d9yqStHdxzvZ">
      <Paragraph id="mJzLBJwQrNihlOWehxbXYs">
        转码
      </Paragraph>
    </TableCell>

    <TableCell id="kHpkQM6QCyPk8xlkB0WyVg">
      <Paragraph id="PLLMI0nlRQfuIoi4nMK49Q">
        ffmpeg + GPU 节点
      </Paragraph>
    </TableCell>

    <TableCell id="UZPRr2ZJI4tSKr64QbiM3H">
      <Paragraph id="o8cBYzGBifdV67pK3lhouA">
        缩略图、HLS 多码率、回忆影片混剪
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="uRJO6J1ucR3yVeoLb08o55">
    <TableCell id="RzLJ7RnpryyzhrcomlzvLV">
      <Paragraph id="tLW83f6W2nJvuaxKg579tR">
        存储
      </Paragraph>
    </TableCell>

    <TableCell id="zS3uxrUxZlSMhGWJLWe0r6">
      <Paragraph id="qcRzhGZNZdrVXjt0Ntt7qK">
        PostgreSQL+pgvector / 对象存储 / NAS
      </Paragraph>
    </TableCell>

    <TableCell id="7g7TSRkVe93uTrFD2lIt8j">
      <Paragraph id="ijktExHDUTN4iGhlidCx7z">
        元数据、向量、原文件、缩略图
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="5Qo4vqDqhBwQIxKUkDpzOW" />

<Heading id="4ljw1HNYT6WSv7Lbp3IP01" level="2">
  3. 服务模块设计
</Heading>

<Heading id="AGAWenLNi91TzYqLEQECwr" level="3">
  3.1 媒体索引服务（Go）
</Heading>

<BulletedList id="abUgFIvw6J1BTbGBz0qsVs">
  <Mark bold>职责</Mark>：监听导入源（SMB 挂载目录 / WebDAV / App 上传）→ 提取元数据（EXIF/ffprobe）→ 写 `media` 表 → 生成 `folder_path` → 触发缩略图与 AI 任务。
</BulletedList>

<BulletedList id="qTXjriTEeLvs521aBIV8Nh">
  <Mark bold>元数据提取</Mark>：`exifread`（照片）/ `ffprobe`（视频）；360 判定：`ProjectionType=equirectangular` 或文件名/宽高比启发式。
</BulletedList>

<BulletedList id="NogHsY65Ex25t2oDa5lrfa">
  <Mark bold>去重</Mark>：pHash 感知哈希入 `media.hash`；索引时发现重复写入 `duplicate_of`。
</BulletedList>

<BulletedList id="MSJ7h2qI758bbPl0PUKbVc">
  <Mark bold>增量索引</Mark>：基于 `mtime` + inode 游标，低峰调度，进度可视（`index_jobs` 表）。
</BulletedList>

<Heading id="F6qLuZ4wlfHTMS1joRAKMH" level="3">
  3.2 AI 推理服务（Python FastAPI）
</Heading>

<BulletedList id="PoiDL7YlsNFxnm1Jk4AfAH">
  人脸：OpenCV Zoo `YuNet`（检测）+ `SFace`（识别，128 维，Apache-2.0） → pgvector 近邻聚类生成 `cluster_id` → 映射 `people`。
</BulletedList>

<BulletedList id="aBe3S6vzYVfLDVXldTRcln">
  宠物：复用同类模型 `is_pet=true`。
</BulletedList>

<BulletedList id="aP69YOX8SdQhRJ67ceKI0B">
  自动标签：`media_tags` + `tags(kind=ai)`（分类/检测模型）；需人工确认 `confirmed`。
</BulletedList>

<BulletedList id="ch7fRRfH1y7MbCHGLs93M4">
  地理编码：国际用 <Mark bold>Nominatim</Mark> 反编码；中国用 <Mark bold>高德(Amap) 地理编码</Mark>（API key 可配置，见 DDL `system_map_config`）。照片 GPS 多为 <Mark bold>WGS-84</Mark>，叠加高德底图需做 <Mark bold>WGS-84 ↔ GCJ-02</Mark> 转换（GCJ-02 偏移）后再写 `media.place`/显示；结果缓存于 `geo_cache`。
</BulletedList>

<BulletedList id="p09cSFypeAHqSbX3z5ZKuf">
  所有模型可纯本地运行（无外发）。
</BulletedList>

<Heading id="hz7Oz30EAPDQPKuDjzkUuD" level="3">
  3.3 转码管线（ffmpeg + GPU 节点）
</Heading>

<BulletedList id="Ur5cXlutBcqwR37T3YcTB1">
  缩略图：多尺寸（SM/MD/LG）WebP。
</BulletedList>

<BulletedList id="KRuCNkYKj9kz3L4ktUguso">
  <Mark bold>HLS 多码率</Mark>（核心）：输入原片 → 输出 1080p/2K/4K 三档 + `master.m3u8`；GPU 节点用 NVENC。
</BulletedList>

<BulletedList id="59tUIbdoxjr9ApRTuXIELT">
  <Mark bold>带宽感知档位</Mark>：`/api/bandwidth` 返回「手动指定」或「自测」得到的实际上下行带宽，`hls.js` 据此在 `master.m3u8` 多档中选取初始档与上限档（见 §4.2、API §16）。
</BulletedList>

<BulletedList id="y3Sk7idPm4iKKweXmaaDvf">
  回忆影片：ffmpeg 拼接 + 转场滤镜（可选模块）。
</BulletedList>

<Heading id="Wzzw55qES6lQrOQnLbm1Iq" level="3">
  3.4 360 播放引擎（自研，核心差异）
</Heading>

<Paragraph id="cmgAM2BQFpUvwKzktv8Pj9">
  详见 §4.2。Three.js 球面渲染 + DeviceOrientation 陀螺仪 + WebXR `immersive-vr` + hls.js 自适应（档位由 `/api/bandwidth` 测得的上下行带宽推荐，见 §6 算力节点与 API §16）。
</Paragraph>

<Heading id="2VEiblvG1PQSiQ7r9QVEqW" level="3">
  3.5 鉴权与权限（独立后台）
</Heading>

<BulletedList id="vMBxYUzqNBGZmByNUdPwvX">
  JWT（Access + Refresh）；密码 bcrypt；可选 SSO(OIDC) 经 Keycloak；2FA 经 `otplib`(TOTP)。
</BulletedList>

<BulletedList id="bIkosk5PNVdxre3i1Jb98i">
  RBAC：`users.role_id` → `roles` → `role_permissions`；共享空间成员经 `shared_space_members.role`。
</BulletedList>

<BulletedList id="letanQY2O7CHyen49kDs1D">
  会话入 `sessions` 可监控/吊销；审计写 `audit_log`。
</BulletedList>

<BulletedList id="k5msbStrzIOHUeTlc3Q5yQ">
  <Mark bold>不对接 DSM</Mark>；账户体系完全自建。
</BulletedList>

<Heading id="EUaGPi4v2j9G3cuPaeq4PA" level="3">
  3.6 分享与微信 H5
</Heading>

<BulletedList id="dJQv1YOmH8lES3Kpztb0Ri">
  `share_links` 生成 token；`/s/<token>` 由反代渲染轻量 H5（不加载后台）；支持有效期/密码/下载开关。
</BulletedList>

<BulletedList id="HFEhzb4uV6JFXmepnzZR2v">
  微信内：复制链接 + OG 封面图；360 视频以 H5 + HLS 播放（非整文件下载）。
</BulletedList>

<Heading id="kgJCTUwognRbDStvND6blq" level="3">
  3.7 地图与地理模块（地图模式核心）
</Heading>

<BulletedList id="0ay1fmJtslDJNgNgVcq7tJ">
  <Mark bold>全屏地图模式</Mark>：默认世界地图；缩放精度至城市级，优先高亮库内媒体最多的城市级区域（按 GPS 聚类计数排序）。
</BulletedList>

<BulletedList id="cch6rDlVT3m94PIzeI2ATJ">
  <Mark bold>底图双源</Mark>：中国用 <Mark bold>高德 Amap</Mark> 瓦片（需 API key，GCJ-02，可后台配置）；国际用 OSM / MapLibre 矢量瓦片（自架 Planetiler 或 Maptiler）。瓦片经反代代理，避免前端硬编码密钥。
</BulletedList>

<BulletedList id="TK8XElGsjazd9nq87Z1P2o">
  <Mark bold>空间聚合</Mark>：`supercluster` 按可视 `bbox` + `zoom` 动态聚合密度圆点；点击展开该地媒体缩略图。
</BulletedList>

<BulletedList id="Nfg4A9UNSBP3vFNKk1GGeV">
  <Mark bold>时间轴直方图</Mark>：后端按 `taken_at` + `gps` 在「当前筛选 + 当前 bbox」下聚合时间桶，返回直方图供前端滑块渲染；拖动/点击切片即按时间窗 + bbox 重查 `/map/items`。
</BulletedList>

<BulletedList id="cX5IpZxkPIXnjONtl7FETW">
  <Mark bold>模糊筛选栏</Mark>：组合 类型/人物/标签/拍摄时间/GPS 范围，文本字段用 `ILIKE` 模糊；结果实时回地图。
</BulletedList>

<BulletedList id="Kmihlco3QX5PyAF8J18siO">
  <Mark bold>模糊地理搜索</Mark>：`/map/search` 经高德（中国）/ Nominatim（国际）返回候选地点并定位飞行；支持中文地名模糊。
</BulletedList>

<BulletedList id="MZAfohuqyXXEbESG8vTApb">
  详见 API §7（地图模式）、DDL §2.5（`user_ui_prefs`/`system_map_config`/`geo_cache`）。
</BulletedList>

<Divider id="xpwENzCaBJk1cF5nVzX9hE" />

<Heading id="L41gKzziAye64AoPAnSkzN" level="2">
  4. 核心流程
</Heading>

<Heading id="uomkvbNQviGqhDaOeT3xqy" level="3">
  4.1 导入流水线
</Heading>

<Code id="Eta1Xs8jCgEcPdpdM2lDOb">
  ```
  SMB/App/WebDAV 写入 → 监听事件/定时扫描
    → 提取元数据(exif/ffprobe) + 360 判定
    → 写 media(space, folder_path, hash 去重)
    → 入队: [缩略图生成] [AI:人脸/标签/地理]
    → 索引进度更新(index_jobs)
    → 完成 → 时间轴/相册可见
  ```
</Code>

<Heading id="i9fGyEtAg2e3kJjdwUU4T0" level="3">
  4.2 360 视频播放流程（自研核心）
</Heading>

<Code id="Lj3YLTB0voKd9Ip1I1gDvf">
  ```
  H5 打开 /s/<token> 或 播放页
    → 加载 hls.js → 拉 master.m3u8 → LEVEL_SWITCHED 自适应码率
    → Three.js 内翻 SphereGeometry + VideoTexture(等距圆柱帧)
    → 控制:
        拖拽/触摸 → yaw/pitch 视角
        双指捏合 → FOV 缩放
        DeviceOrientation(需 iOS 授权) → 陀螺仪跟随倾斜
        WebXR requestSession('immersive-vr') → VR 头显左右分屏+头追
    → 全部交互强制 HTTPS(微信/头显要求)
  ```
</Code>

<Paragraph id="fagxrfyGpiBnaBH86nmzT5">
  <Mark bold>360 引擎详细设计补充</Mark>：
</Paragraph>

<Table id="KxnUcsIASdwKC45o1irWrG" readonly rowHeader>
  <TableRow id="EmFUPPP0f34T0rlASqqjlu">
    <TableCell id="mHVpPG1Tc1ad2XALMrFFpp">
      <Paragraph id="gkzGCkn24czqRgPvVf6m4Y">
        子系统
      </Paragraph>
    </TableCell>

    <TableCell id="8hv9xIYPG5PJeAQVu7CYPC">
      <Paragraph id="rTc88qVn6T1O6bU7bDQWYn">
        设计要点
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="LO0J1eRkECqy46f9LUyCuo">
    <TableCell id="kltbL8IVyoNb8s6pjkiIZO">
      <Paragraph id="npdjjnvuj31vwHzlcVPCW5">
        <Mark bold>球面渲染（WebGL Shader）</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="iQVF8anudl7nRkAC4cHOAi">
      <Paragraph id="RnioQdiJqqu9vHZ6zmDgOB">
        `SphereGeometry(radius=500, widthSeg=64, heightSeg=32)` 内翻（`scale.z = -1`）；自定义 `ShaderMaterial` 翻转 UV.y（`vec2(1.0, uv.y)` 适配 equirectangular 帧）；`VideoTexture` 设 `flipY=false`；球心相机 `fov=75°, near=0.1, far=1000`
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="A8X3eRhJBgFtPGKkZtSqHH">
    <TableCell id="vabBmpgvG9klX4w6hZ7XDD">
      <Paragraph id="hH0MOIW9u3QalhiMMDp1H8">
        <Mark bold>iOS DeviceOrientation 权限</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="5vErm0Rp5d7eKd4Zg84Ob5">
      <Paragraph id="wFz5LLXJDtuCpmlPNghgMw">
        iOS 13+ 需用户手势触发 `DeviceOrientationEvent.requestPermission()`；播放页加"开启陀螺仪"按钮，点击后请求权限并绑定 `deviceorientation` 事件；权限拒绝后降级为纯拖拽模式
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="v4yuWWeUuZnyj4I92TCujn">
    <TableCell id="fHToBj20YbPGYlc4V2TFMx">
      <Paragraph id="4gm8gB3T6CAUeqYpPCT8tg">
        <Mark bold>WebXR 会话生命周期</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="xUg8H5YxWcxRX9skdxBHzr">
      <Paragraph id="tmaSZj7anc5FFQ7nVDlM5w">
        `navigator.xr.isSessionSupported('immersive-vr')` 检测 → 用户点击"VR 模式"按钮 → `requestSession('immersive-vr')` → `session.requestAnimationFrame(renderLoop)` → 退出时 `session.end()`；`sessionend` 事件清理资源并恢复标准渲染；会话中断（电话/切换App）自动暂停视频
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="lOmgqaz0tfj9kVQ8pCUWsK">
    <TableCell id="lTOH4Ckr98V98VfNI3f6az">
      <Paragraph id="okOca5UIjQNec2peowjvay">
        <Mark bold>hls.js 错误恢复</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="wQwWG9sSZSeQHUgFPPg80R">
      <Paragraph id="bSi5IDH94XCpvu77Y3FUEj">
        `Hls.Events.ERROR` 监听；`fatal=true` 时：网络错误→指数退避重试（3次）；媒体解析错误→降级到下一档或恢复到最低档；不可恢复→显示"请检查网络"提示并保留最后帧
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="AP8o0Rf5fJqZQjv36gcaX3">
    <TableCell id="1YEjfzi4edLZ3Q9LzSpkqL">
      <Paragraph id="E2SiVoJwSKCbc5l66mZ6Ys">
        <Mark bold>移动端性能优化</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="HfBlBnSGAGj5dito4Hp9Fq">
      <Paragraph id="bgVD1misBsChzd4V65sWM5">
        纹理尺寸上限：移动端 `maxTextureSize=4096`（4K equirect 需分块或降采样）；帧率自适应：`requestAnimationFrame` + 性能监测，低于30fps时自动降档；`powerPreference='high-performance'`；VR 模式锁定 `75fps`
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="sQrbskFKU5VPwbNx8wwAvu">
    <TableCell id="qMSiLHYobdrN7Bt2Oqo70D">
      <Paragraph id="vR6alDTnIUP7cNHyadKk97">
        <Mark bold>视角同步（多端）</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="RZ5dH0V3dHsqoKUZnyyTDy">
      <Paragraph id="GhiSvbWnLQSW4o6164X0eb">
        可选 WebSocket `/media/:id/360/state` 同步 yaw/pitch/timestamp，用于投屏到头显时手机作控制器
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Heading id="aHpO0GBlZcFBATa5i0sifB" level="3">
  4.3 分享流程
</Heading>

<Code id="8AT198as4CtAjMJ3i7mZlF">
  ```
  选相册/媒体 → POST /api/share → 生成 token + 可选密码/有效期
    → 返回 /s/<token>
    → 微信: 复制链接 + 右上角菜单分享 + OG 封面
    → 访客打开 H5 → 免登录观看(360 走 4.2)
    → 统计写 share_access_log
  ```
</Code>

<Heading id="20F1iltdUkQ2e7EgI4A3YF" level="3">
  4.4 鉴权流程
</Heading>

<Code id="klJZ3b5X1jRyu0tLPwuq0c">
  ```
  登录 → POST /api/auth/login → JWT(Access+Refresh)
    → 后续请求 Authorization: Bearer <JWT>
    → 网关校验 + RBAC 判定(角色/共享空间成员)
    → 2FA 首次/敏感操作需 TOTP
    → SSO: OIDC 回调换取 JWT
  ```
</Code>

<Divider id="GMb4A7JogzuaM7QggflQgx" />

<Heading id="KAdQiEZ0M1FnyY42YOAnfn" level="2">
  5. 里程碑（与 PRD §5/§10 对齐）
</Heading>

<Table id="w6ZO1z9FosjujQb5SNgdHG" readonly rowHeader>
  <TableRow id="z6LtmgS5jntjKlC6zn2UF0">
    <TableCell id="8oCU1TJvhlf7nzSmYRXUrs">
      <Paragraph id="uXR9dTKP0ZqGtYTc4vexnY">
        阶段
      </Paragraph>
    </TableCell>

    <TableCell id="QOrDujMRb1uCU4HZEbEqyV">
      <Paragraph id="RBCOGaffqzd8xgu3BgO5i1">
        交付
      </Paragraph>
    </TableCell>

    <TableCell id="gaohtnF1T7uEACxhWFGZZJ">
      <Paragraph id="bC22POJT334cYRBfVvr6Y9">
        验收
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="8OhV2VTHpaPmYmIyVQ2Par">
    <TableCell id="iuSi6HV0FAE41Npu8i5tPf">
      <Paragraph id="eiqU7spb3nRmq2P35gu5Ut">
        P0
      </Paragraph>
    </TableCell>

    <TableCell id="gugxfqaTQeoTUSeWCJDO00">
      <Paragraph id="kLwLbIi3iYBM3fmEy89Qmd">
        时间轴+年日月+索引+缩略图+双空间+文件夹视图
      </Paragraph>
    </TableCell>

    <TableCell id="PxPq9T4hfe6aOAJZuymjRy">
      <Paragraph id="yYO0y6vEM0ZqaGJECiIUJU">
        群晖级浏览
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="7sEl4LHPrAeMV6qF5YcoEm">
    <TableCell id="5DzPZsJR2ogzKX6lxi2QMo">
      <Paragraph id="lm0piHfukZh0NoPVTcD6z5">
        P0
      </Paragraph>
    </TableCell>

    <TableCell id="TSjyBis8vD99aWV9rEhjso">
      <Paragraph id="L8rJoMC9M5e8nksefysjTT">
        ★ 360 播放引擎（陀螺仪/VR/ABR）
      </Paragraph>
    </TableCell>

    <TableCell id="IeivGqY1gxm23x21iv9GDV">
      <Paragraph id="y8CEuZ89dW3Ak6ZZx91zsk">
        iOS/Android/Quest 可交互
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="SdklYSSMTIcCojhd3xez4l">
    <TableCell id="CmxClaRHj2Yv9rkAsdgfHN">
      <Paragraph id="Hbkxfmaj1c3xEwngUcPaoN">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="XuZ75fGfaWDwi3BVUJJ4vH">
      <Paragraph id="93ffh3ky0KgmFqcwFdtZ7T">
        相册/人物/地点/标签/文件夹/查看器/幻灯片/基本编辑
      </Paragraph>
    </TableCell>

    <TableCell id="8tpzhyL6Ih7r80mRFfbqe2">
      <Paragraph id="Xz3qh7mYK7MphEnh47MEPk">
        自动聚合准确
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="tOo0DFMFRcn5Vn0uNyzFAC">
    <TableCell id="hR6EMo2wIvj6ORtXCYJ2zm">
      <Paragraph id="OTo1yN3doZS5i2FQn5ItHI">
        P1
      </Paragraph>
    </TableCell>

    <TableCell id="QbhMtWXRQrhHMXsDqUZEZA">
      <Paragraph id="A9AevF4g42oMUOUyOAMRJR">
        HLS 转码/搜索/微信H5/移动端备份/360照片
      </Paragraph>
    </TableCell>

    <TableCell id="LrU4T5KkkgAqr76KTD7IKo">
      <Paragraph id="k8RFsO5WwlXlBPdxriasw5">
        编辑可用、远程自适应
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="m069UmGnCC0BjJOeKStyfS">
    <TableCell id="IhWcUAr1K4nNQbEZwozw4n">
      <Paragraph id="w3yEgMlHDv1845zC551LxI">
        P2
      </Paragraph>
    </TableCell>

    <TableCell id="xhVl8EujEaKrzQXS86XHS3">
      <Paragraph id="bSDhWAjxIIoRUQtC2eiSeQ">
        多用户/SSO/2FA/审计/3D地图/工具箱去重/回忆影片
      </Paragraph>
    </TableCell>

    <TableCell id="eYbmcaBDixfrDFs4eMQN0Y">
      <Paragraph id="tZ3426KDqnmkn24BiZtYet">
        企业级权限
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="ngisOsWDcntVFzzqZRt26q" />

<Heading id="Jw6aiwLvIOIjyJWO5wfrkc" level="2">
  6. 存储与算力分离（关键约束）
</Heading>

<BulletedList id="V1IrDrrt8mg2ByvsuemZne">
  <Mark bold>DS1819+（Atom C3538 无 GPU）</Mark>：仅作存储卷（NAS 文件系统或格式化为对象存储 MinIO 后端），<Mark bold>不参与 AI/转码</Mark>。
</BulletedList>

<BulletedList id="Qac5ptiT5UDg3xoWzSeysI">
  <Mark bold>算力节点（Compute Node，多形态抽象）</Mark>：引入统一抽象层 + 调度器，支持三类接入并在配置中切换：

  <BulletedList id="bVqgZNWBn4zUpl5DTjSZYF">
    ① <Mark bold>本地 GPU</Mark>：本机/同机容器（含 NVENC）；
  </BulletedList>

  <BulletedList id="BqLfonFgFqE31NNSkt3JbL">
    ② <Mark bold>云 GPU</Mark>：按需云实例 / Serverless GPU（弹性、用完释放）；
  </BulletedList>

  <BulletedList id="hCGTVWQqszGcp7czTqCdhS">
    ③ <Mark bold>本地网络第三方 GPU 主机</Mark>：局域网内 Windows / Linux 主机，经 <Mark bold>agent（长连接心跳 + 任务拉取）</Mark> 接入。
  </BulletedList>

  <BulletedList id="K2rrDfTiZs99vz0x9oX8nR">
    节点能力声明：`codec` 支持（h264/hevc/av1）、是否 `nvenc`、显存、`concurrency`。
  </BulletedList>

  <BulletedList id="cXB00hDAQsWyJZRCoVuyN6">
    调度：注册→心跳→能力匹配→任务入队（`transcode_jobs`/`index_jobs`）；节点掉线自动剔除、任务重派。
  </BulletedList>
</BulletedList>

<BulletedList id="YfVC3emQZAy8bxFFw9NTc5">
  <Mark bold>边界</Mark>：原文件存 NAS；缩略图/HLS 可存对象存储（同机或边缘 CDN）。
</BulletedList>

<BulletedList id="Mam0FPeVwZq0dqJA6U37f9">
  <Mark bold>理由</Mark>：避免 C3538 纯 CPU 跑人脸索引与 4K+ 全景转码卡死；多形态节点让算力可随预算/网络在本地与云之间弹性调度。
</BulletedList>

<Heading id="Djs0Sc6GPc7IhWgOa4S8wt" level="3">
  6.1 算力节点 Agent 协议
</Heading>

<BlockQuote id="21BeiGDymuAs0l69E71jqm">
  <Paragraph id="7r8CGNdsGyhYzPdewR3CX3">
    `lan_agent` 类型节点（本地网络第三方 GPU 主机）通过 agent 守护进程接入，本地 GPU / 云 GPU 也可使用 agent 统一管理。
  </Paragraph>
</BlockQuote>

<Table id="eMPL4E2jkqd5en0hzkojgt" readonly rowHeader>
  <TableRow id="Usk2IFFpRgFhMPARacwKF1">
    <TableCell id="RphOzP9iIPJf6EPEEOW9bS">
      <Paragraph id="Y2tVvNcN3hY60tJ4xCc6ul">
        协议要素
      </Paragraph>
    </TableCell>

    <TableCell id="fdToJJP9PRDMKOveqSesAC">
      <Paragraph id="7K7WOqV8BWZMim5hPK2rmH">
        设计
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="EmIbi73ziCZrjQe7AX229V">
    <TableCell id="10ygBNVpEZDB101giQmptg">
      <Paragraph id="JDRCHOY5srygg73J598VSG">
        <Mark bold>传输层</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="wF6ZdFXFtnLQiTN2Bu5osm">
      <Paragraph id="0qozzm3ktfjojpp9kIh2zl">
        WebSocket（wss://）长连接；备选 gRPC 双向流
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="JwuDzDE40kwnoV7Qo5bXtJ">
    <TableCell id="eQ12EhgfTsT1PKxTErJ8fd">
      <Paragraph id="XnrKCHs4as27YoJdSIKI92">
        <Mark bold>心跳</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="XU0Zol3O2M0wafBBZm7RcF">
      <Paragraph id="dng9exvW3Ce1OzfRG6Ipgn">
        每 60s 发送 `{type:"heartbeat", gpu_util, vram_used, active_tasks}`；3 次未收到 → 标记 offline → 重派其队列任务
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="M784ImXK0Z8LlvAwa9zexw">
    <TableCell id="nU8wWiFHVcjW7ABAyFv38t">
      <Paragraph id="CRQispB5LSbGH5ia2IGlkA">
        <Mark bold>注册</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="bMjTRaiuPLDSzqzIqaqpfr">
      <Paragraph id="MQXc0Utae8RLGvLbvtAHNV">
        首次连接：`POST /api/compute-nodes` 获取 `agent_token` → 连接时 `Authorization: Bearer <agent_token>`
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="wvgxucfLJjDAKdEQYHFd5g">
    <TableCell id="y55pB2ihVVIqlVGfZWsqVZ">
      <Paragraph id="YFZ3wMS5q2MavACJgf38vp">
        <Mark bold>能力声明</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="VyJh6Lf0Hlfi51PPclz9s2">
      <Paragraph id="7BQUyFknggcp4Y0rSD7KOm">
        注册时上报 `{codecs:["h264","hevc"], has_nvenc:true, vram_mb:24576, concurrency:2, cuda_cores, tensor_cores}`
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="lNQFGpM7oCCpHhb6wmYlgE">
    <TableCell id="FnnJv8nZpzFNtIEUxt3FSZ">
      <Paragraph id="ekmEtF9EUgdZD0Pefh3FRA">
        <Mark bold>任务拉取</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="M3PTK30eHx4dR6hbRis7k2">
      <Paragraph id="NCUq1jW9XcTBWZiDtWMj9K">
        agent 发送 `{type:"poll"}` → 服务端返回 `[{job_id, kind, media_id, profile, input_path, output_spec}]` 或空
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="3C2FLZYetGoV7AoOVFxdH3">
    <TableCell id="mOxF0scSoMESvyujB11Oqa">
      <Paragraph id="u658jAjRsAjbsvIIhy1IIy">
        <Mark bold>结果回传</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="4hwXjhapKp2BmaZhE7ttfn">
      <Paragraph id="uQsycF3wL8T3WkzA48kgeY">
        任务完成：\`{type:"result", job\_id, status:"done
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="L1kkR8V8jrPIYZsh0fErI5">
    <TableCell id="8DwF0jJ9SCyO5eUHIArPpo">
      <Paragraph id="2OuOZz2crHIYZGfLNle0NB">
        <Mark bold>断线重连</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="tgXEm6AjMgVclwhQTHV6Zx">
      <Paragraph id="2unakzQ0W0eFkKdjfUUsBB">
        指数退避：1s→2s→4s→8s→30s 上限；重连后重新注册能力 + 拉取未完成任务
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="xeXPJVavLpVXl85OpA3gvb">
    <TableCell id="xbMVc1y7wv8henMkAXHDBZ">
      <Paragraph id="u4Is3kqbiKjbaT3WorM2uQ">
        <Mark bold>安全</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="IUt7gJTuOE2tvYeBcwgHQE">
      <Paragraph id="xq3AalwhBWXKtTIDSGw0aZ">
        wss 强制 TLS；agent\_token 轮换（管理员触发）；内网可配 IP 白名单
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="TbDmRbpI5XxmBvgfNYmTB5">
    <TableCell id="5Vt6dzgG7EkQaVztamZd74">
      <Paragraph id="5TZ0F9KRLxhpm3DAeBZnGt">
        <Mark bold>平台兼容</Mark>
      </Paragraph>
    </TableCell>

    <TableCell id="1PWjvImOF1tLWrKexFXc2r">
      <Paragraph id="udi7w8wAiJtQWccTbSlggN">
        agent 二进制覆盖 Windows（.exe）/Linux（ELF）/macOS（Mach-O）；配置文件 `agent.yaml`（node\_name, server\_url, token）
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="q7l4a7SzdZhPwqYZx2txif" />

<Heading id="3Nu0hfO5e9WHQNBZMovzZb" level="2">
  7. 安全设计
</Heading>

<Table id="KsEvk8ruQGkz7ikjySxJlH" readonly rowHeader>
  <TableRow id="3RVcOr7FbXE2sh7u8VVeOD">
    <TableCell id="8Z09v8MbAJWClKq8lsGNCi">
      <Paragraph id="gIKUFL0gJToT8kimfJhWIR">
        项
      </Paragraph>
    </TableCell>

    <TableCell id="gSLTu5hPnalREWyFRuNrkn">
      <Paragraph id="ycL92vYn0Viqkw7tnktI5K">
        方案
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="vNkJluUQTIEE4Ex5ufpniL">
    <TableCell id="csUtkoUNk9Lwzulztiz5DE">
      <Paragraph id="50whavsFPqkCYXRkyQtpzY">
        传输
      </Paragraph>
    </TableCell>

    <TableCell id="G94AUwxsqmADJaE6KCXId9">
      <Paragraph id="hiDv6mtSjy20LYnAn3BHpR">
        全链路 HTTPS；<Mark bold>支持自定义自用域名 + IP 直接访问 + 非标端口 + 路由器端口转发映射</Mark>（Caddy 监听非标端口、透传 `X-Forwarded-*`；分享基址 `external_base_url` 可配置含端口）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="sKI4tawK9eECtTUTcINfgo">
    <TableCell id="1oSfelZLBKcwQKVurATRow">
      <Paragraph id="o8YbOhDw2mdIQe20xdmorD">
        认证
      </Paragraph>
    </TableCell>

    <TableCell id="uzERLSWrqOzmFIa0YpH9df">
      <Paragraph id="BSFnyp9xfKEVTu0jJcO7fK">
        JWT + Refresh；bcrypt 口令；TOTP 2FA；OIDC SSO
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="EFbSB5YIwntGftBVMopNnX">
    <TableCell id="2Av7nKHEyjhs0t3t3Q1A7U">
      <Paragraph id="vuemUwRApzfYKjGesjRUCq">
        授权
      </Paragraph>
    </TableCell>

    <TableCell id="ASU9GvpHxG5lpXHgY3ESzH">
      <Paragraph id="dXVFTBHYG5lGnwiYsfVlmL">
        RBAC（角色/权限表）+ 共享空间成员角色
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="h14vNxdDhKMHIg6pTCjWTO">
    <TableCell id="6ihuruCVtqnqlA1axqGWtX">
      <Paragraph id="qz8KfvYG6gHZ83mvaahscM">
        限额
      </Paragraph>
    </TableCell>

    <TableCell id="JV36xh7Bs19jgjDSf6Ud5q">
      <Paragraph id="7quHRYsy5BwfiNqQ7sm5GT">
        Redis 令牌桶（登录/IP/API）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="8DDYtxWPfJMUHXb0rUTSWh">
    <TableCell id="NvbylsoazdIhLIRYltJcHP">
      <Paragraph id="zWrbdOksQpBnoZChuENYJ1">
        会话
      </Paragraph>
    </TableCell>

    <TableCell id="85V8ZIIRcvOVVjB33P6pmM">
      <Paragraph id="TtNpjYuMJYN0Gva6pnKcXs">
        `sessions` 可监控/吊销
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="fjW414xMNsSi821nnK64zg">
    <TableCell id="8vKbjPDSejPV8IYliqm1tP">
      <Paragraph id="G8iF4HoUT0iIY1soys8sma">
        审计
      </Paragraph>
    </TableCell>

    <TableCell id="96zlYuHf3xHyApzQtp8Lp1">
      <Paragraph id="1iDECDu0NC7DqLiAUSoCVW">
        `audit_log`（用户/动作/时间/IP）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="bmFfy0QA6hqP0BCFwDfmQA">
    <TableCell id="lHlNXLGjrY6OaGmj0eAX10">
      <Paragraph id="IY9zsHn5p3NPZsDAWg0plm">
        隐私
      </Paragraph>
    </TableCell>

    <TableCell id="33szHN359qtSvQ43xTOH8X">
      <Paragraph id="3CI0qmC3jhx8D6hyjAQg4U">
        AI 推理可纯本地；数据不出域
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="qUsKFa2oqeBT1v9Tftsq0s" />

<Heading id="08bKcemQKoFpFRnBE17bAP" level="2">
  8. 运维与可观测性
</Heading>

<Heading id="B71xOdgHlgOiHf5FrPqDfb" level="3">
  8.1 监控与告警
</Heading>

<BulletedList id="3TZdJtS8Qgvc2kxtrj4mTj">
  <Mark bold>指标采集</Mark>：Go 服务用 `prometheus/client_golang` 暴露 `/metrics`；Python AI 服务用 `prometheus_fastapi_instrumentator`。
</BulletedList>

<BulletedList id="Bo94feWKEEiLcKZJjvcAfY">
  <Mark bold>关键指标</Mark>：`http_request_duration_seconds`、`media_index_total`、`transcode_jobs_active`、`gpu_node_online`、`hls_bandwidth_kbps`、`face_clustering_duration_seconds`。
</BulletedList>

<BulletedList id="o3Gmtx6ClaS9wvQBa4q3UV">
  <Mark bold>面板</Mark>：Grafana Dashboard（预置模板：概览/转码/索引/360播放/地图）。
</BulletedList>

<BulletedList id="N4L8Y64sNWGWNkCEbIwDUV">
  <Mark bold>告警规则</Mark>：节点离线 >5min、索引队列积压 >1000、转码失败率 >10%、磁盘 >85%、证书 \<14天过期 → Webhook → 飞书/邮件。
</BulletedList>

<Heading id="X1Dh72DOsnxkEScxMlgTFF" level="3">
  8.2 日志体系
</Heading>

<Table id="WaBLtMJiFQks2KG37gsPh2" readonly rowHeader>
  <TableRow id="LdgvmUkntwrKIBLRh9WPeu">
    <TableCell id="OApBIcLoNYMo2VAj4xe9Fc">
      <Paragraph id="fhYLFdPoWEkDniKWD1bnTc">
        日志类型
      </Paragraph>
    </TableCell>

    <TableCell id="TaHq3q7rStxTtAF1TzzjrU">
      <Paragraph id="1mKyonsmSktBFnM0JShgjX">
        格式
      </Paragraph>
    </TableCell>

    <TableCell id="VrmijvbumZVfiK64NZbcs7">
      <Paragraph id="lHt86BPbUj7RO6R2OzlqzB">
        存储
      </Paragraph>
    </TableCell>

    <TableCell id="PbGoQEgkHp2RDfmZErXsc8">
      <Paragraph id="D1ZnbwUpEsiD3GKUxDC3W9">
        保留
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="3SO7r5khAlxtiMZCPBQcZR">
    <TableCell id="w6AwOiZ4yuANUbfA7zFgPu">
      <Paragraph id="6DiKM6KxdumIOS1T2ncM8s">
        应用日志
      </Paragraph>
    </TableCell>

    <TableCell id="mKYbIgIXwnlpv6VR0RbnkA">
      <Paragraph id="l5WAdDir2Xj87SaFdslL27">
        结构化 JSON（`level, msg, request_id, user_id, module, latency`）
      </Paragraph>
    </TableCell>

    <TableCell id="WkzySqXqp2gL8I56oZJlqV">
      <Paragraph id="1w3rMDG9LHAwblg50Im0Fv">
        文件 + Loki
      </Paragraph>
    </TableCell>

    <TableCell id="7MaAIQPyIGBUiryVUF7BJ4">
      <Paragraph id="RNnriaXT6WDtm5pzpcySJo">
        30 天
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="D0WSPwA2sRjdxYV9PHaJVO">
    <TableCell id="2HQG40JnYXKzMBWGfgfbCC">
      <Paragraph id="C2x0Xker80FvIKl6fJOqDK">
        访问日志
      </Paragraph>
    </TableCell>

    <TableCell id="GCU8xkV8gFGurwG8P5Rq9G">
      <Paragraph id="83qMhU0IwZXRw6IDmlABiA">
        Caddy `access_log`（含 IP, UA, path, status, latency）
      </Paragraph>
    </TableCell>

    <TableCell id="OHM13Meo4gmx4GyWehWRRL">
      <Paragraph id="1fSzjrY63XLM1HhlgCmVJj">
        文件 + Loki
      </Paragraph>
    </TableCell>

    <TableCell id="BkRJZlF9lF7wxIZR1eSyse">
      <Paragraph id="7kJI0Edj6XoO4FMO5Cb0Je">
        30 天
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="dJzlUPrXARnownyPyvNwfa">
    <TableCell id="iUJNsa2vSSn4pgY76UOw0Y">
      <Paragraph id="zFEDeLwjnlTufVQxDVLrYn">
        审计日志
      </Paragraph>
    </TableCell>

    <TableCell id="1eRAD7aLcIr4j5uOMmtfGU">
      <Paragraph id="Re6IvdG2nyhrJFZcOcFrda">
        `audit_log` 表（DDL §2）
      </Paragraph>
    </TableCell>

    <TableCell id="WoT9JgeUsVLd104DfJOzfN">
      <Paragraph id="Cf61V50jd54VUbtWKgnkD1">
        PostgreSQL
      </Paragraph>
    </TableCell>

    <TableCell id="DKHCj0j8WUIHZecIF35tJz">
      <Paragraph id="kf2bs1F47RGkFHLgLoi2Dt">
        永久
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="J8suyMscJmoC3XT8FxbwFG">
    <TableCell id="PcJ4miYwTvRPSTRarhCz2m">
      <Paragraph id="emlhWabW1i9NJ86sKE9GLO">
        错误日志
      </Paragraph>
    </TableCell>

    <TableCell id="y6BrXDzryahU9TYi7PpuDH">
      <Paragraph id="EICNrFZIC3BdBskaiuys7E">
        Sentry（可选 SaaS 或 self-hosted GlitchTip）
      </Paragraph>
    </TableCell>

    <TableCell id="KWkBMK7AFPLwEHI1GzixQe">
      <Paragraph id="1cHGWlFk6psoBjX83DfdIg">
        Sentry/GlitchTip
      </Paragraph>
    </TableCell>

    <TableCell id="t0g6GXlgrWz8TkYEy2jnjS">
      <Paragraph id="ytyzROvLeZvnZSPsNH6rTj">
        90 天
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<BulletedList id="ccOd6Wgoa1aFoWGOh37Bko">
  日志轮转：`logrotate` 或 Docker `json-file` max-size=50m max-files=5。
</BulletedList>

<BulletedList id="X9IRctHbNc4bqvFXQzAGT6">
  链路追踪：`request_id` 贯穿 Caddy → API → AI/转码 → 日志关联。
</BulletedList>

<Heading id="til8oMbUkqzsZAW3BKkCt8" level="3">
  8.3 备份与恢复
</Heading>

<Table id="Udu5fP2OY9dY58UCCA2Uwb" readonly rowHeader>
  <TableRow id="NJIhbV81e7y38Cf33o4tAQ">
    <TableCell id="45GqNHrgJvSfZYrG2uwC4g">
      <Paragraph id="paFzsRp3LWsPdM9wuDpD9W">
        备份对象
      </Paragraph>
    </TableCell>

    <TableCell id="NHCXdgDG4BWqGiNhV1X4Nr">
      <Paragraph id="AAXeiVUv03dPpiwDWrRu4B">
        方案
      </Paragraph>
    </TableCell>

    <TableCell id="mCOeQbbuwFQiFz6mXrVaEz">
      <Paragraph id="NaGWEdmPMIEKDeIcCAT4C1">
        频率
      </Paragraph>
    </TableCell>

    <TableCell id="9lnCshx4fDLTfUxo37jUlb">
      <Paragraph id="NfUIp7V6hJkgUeL4Bxfuwp">
        保留
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="vUfhmgJVgjuO9jkmS7yMLM">
    <TableCell id="E6P1qQNstG8zNK8dnsrzsf">
      <Paragraph id="8xqQmE9wcvCRyMbHCXQzI7">
        PostgreSQL
      </Paragraph>
    </TableCell>

    <TableCell id="x5sT4uFtHBW6HqwR75L45b">
      <Paragraph id="eAXORFrjppoUcDPeoPDMsH">
        `pg_dump` + WAL 归档（PITR）
      </Paragraph>
    </TableCell>

    <TableCell id="ZRPxZoLfNgui0LMMijiLdS">
      <Paragraph id="91n6LlNDO8GQAGjbS1sbTe">
        全量日1 + WAL 实时
      </Paragraph>
    </TableCell>

    <TableCell id="FcnUgbVPPZUomo5L5szXTu">
      <Paragraph id="zk8jb2b0X83HD9wSgbbaOF">
        30 天
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="2HizmhrjtuUqrIyzksuUsS">
    <TableCell id="FvuUMT5YSxuwUNxtpJNvDz">
      <Paragraph id="wHxufVRcuQ6caGq8YroN6O">
        对象存储（原文件/缩略图）
      </Paragraph>
    </TableCell>

    <TableCell id="2SJuP7631qQSxLmvkZgrHc">
      <Paragraph id="3KqeoeKt9ktT0nIzILUdnL">
        MinIO 版本化 / S3 生命周期
      </Paragraph>
    </TableCell>

    <TableCell id="S5OIlAquW69bBt7BJ9XdKI">
      <Paragraph id="smyqIbTFgGCFjXYacqDdmO">
        实时版本
      </Paragraph>
    </TableCell>

    <TableCell id="tMj4QEy2cyGpx6Xn6n5AGG">
      <Paragraph id="XuS4S8e1GZz1wYlHNWKr1y">
        90 天
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="32EPj36itYx4YQHfDvVadk">
    <TableCell id="UyOtXeRLrlLF68DXa88rIj">
      <Paragraph id="e1pTa4rMMk0459EbPoRcdA">
        系统配置
      </Paragraph>
    </TableCell>

    <TableCell id="OVhXH9ECebobuv9HnOf1Lz">
      <Paragraph id="VPmhA1SPTavZX8DRHFPQd7">
        `dsm config export` + `docker-compose.yml` + `.env`
      </Paragraph>
    </TableCell>

    <TableCell id="LaY7lMufg4SaoNSy5y6MTa">
      <Paragraph id="cVNEe4mawXOK2FD7CXFvdX">
        变更即备份
      </Paragraph>
    </TableCell>

    <TableCell id="hMSmNWmS3ELvxCh1V8UmXB">
      <Paragraph id="YyIe0geDRCjUpHNzZsQfKb">
        永久
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="6Q1fJUShj9CrmngXK51W2e">
    <TableCell id="yXWQhcIvN42ep7U8bJe9tb">
      <Paragraph id="10ITuFtbF84ybVZrRWPEfX">
        DDL Schema
      </Paragraph>
    </TableCell>

    <TableCell id="Sd16KQUZYSmfIVBcubqI15">
      <Paragraph id="bxpXR3q9IUTBykhpGRw73u">
        `pg_dump --schema-only`
      </Paragraph>
    </TableCell>

    <TableCell id="9Qqsh4aIQrp1TKjKfAlzkW">
      <Paragraph id="aVe6vV7LE3WuiEdsWJ0nnT">
        每次迁移前
      </Paragraph>
    </TableCell>

    <TableCell id="ySnc5TUbo8kd1XUTLROA2A">
      <Paragraph id="rMm2loSKu64m0e4HlKKa9h">
        永久
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<BulletedList id="RhyMepm0e4r6qkehiXd7tz">
  恢复演练：每季度执行一次全量恢复测试（模拟磁盘损坏→从备份恢复→验证数据完整性）。
</BulletedList>

<Heading id="h4PS3p3n7tedHX4nWqKALy" level="3">
  8.4 CI/CD 流水线
</Heading>

<Code id="f6p4c66Ezb76KzYN8oBtWC">
  ```
  Git Push → GitHub Actions / GitLab CI
    → lint(go vet, golangci-lint, eslint) + test(go test, pytest, vitest)
    → build(docker build, multi-arch: amd64 + arm64)
    → scan(Trivy 镜像漏洞扫描)
    → push(registry)
    → deploy-staging(自动) → deploy-prod(手动审批, 灰度)
  ```
</Code>

<BulletedList id="ybyFivB14bEK9LnNVgRZ6u">
  灰度策略：先部署 1 个 API 副本，观察错误率/延迟 10min，无异常后全量。
</BulletedList>

<BulletedList id="YcIgLwnMs8G1MCJrn4u5Kc">
  回滚：保留前 2 版镜像，`docker compose up -d --no-deps --force-recreate api` 一键回滚。
</BulletedList>

<Heading id="qkdeYTejIMTrv7OxBqxLeG" level="3">
  8.5 健康检查与就绪探针
</Heading>

<Table id="lXrEZ3bI9KnjCxs0HX7YGV" readonly rowHeader>
  <TableRow id="LE6Jsg9ATSlH7DgLVWu0fn">
    <TableCell id="zWFrGv3tlz0buU4umRmhHp">
      <Paragraph id="Nsw84OXx4tEBtlMQW9fRmV">
        端点
      </Paragraph>
    </TableCell>

    <TableCell id="40Yfgr6OLyeZhXpeD9AA88">
      <Paragraph id="L7M2AG8L7h4cHmIGWV0eBd">
        用途
      </Paragraph>
    </TableCell>

    <TableCell id="4vu7H2BFa4twF3jnfyqFf3">
      <Paragraph id="2N5AASxWatWhr54OOrp5i8">
        检查内容
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="sMptzJx1M7DXWlcsqP3nGn">
    <TableCell id="D5loOPMUIEirZ33uG3nt7Y">
      <Paragraph id="wHoXCAFxMNP77y9cXXIdHq">
        `GET /health`
      </Paragraph>
    </TableCell>

    <TableCell id="xA5kvpQLRrQOiu3qoyAKhn">
      <Paragraph id="UXT3FnPibjwpJhte8aV1A6">
        liveness
      </Paragraph>
    </TableCell>

    <TableCell id="FxgiR8OC3DWz9IGQbioPdR">
      <Paragraph id="9jrK9PvPpWKvxI89PngYgZ">
        进程存活（返回 `200`）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="sdI08th9oj8uqym2BTTMna">
    <TableCell id="Tqqg5cal042W66CBUmiSUI">
      <Paragraph id="duCVoyfRcTGud8VFTX6Spy">
        `GET /ready`
      </Paragraph>
    </TableCell>

    <TableCell id="UGsxJ2R7pUiFeytYSNHmKB">
      <Paragraph id="Z6MH8NGCYH90Bn6PmpNDNl">
        readiness
      </Paragraph>
    </TableCell>

    <TableCell id="fqeV6pCwcNOGSy6nMn0ryT">
      <Paragraph id="RbNMe6kVZeTlANLAX6s5S7">
        PG 连通 + Redis 连通 + 磁盘可写（全通才返回 `200`）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="3uf8PjttHo8HnbliDJhM1a">
    <TableCell id="SUrtaikEceRbsyRFJZzsHS">
      <Paragraph id="849OF5HSSEPXQEfqSH0Ywy">
        `GET /metrics`
      </Paragraph>
    </TableCell>

    <TableCell id="OKHeq1279mGo0FO3rLzBKP">
      <Paragraph id="fllVMjVoyY2YMMKfqSTsJo">
        Prometheus
      </Paragraph>
    </TableCell>

    <TableCell id="8dy2W5iSZc7jA5LGXRFn63">
      <Paragraph id="ErPtR0Kv0P6uEHn223GXPI">
        指标暴露
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<BulletedList id="4fbZpPQtW1wZ0eQ8cYyDs2">
  Docker: `HEALTHCHECK CMD curl -f http://localhost:8080/health || exit 1`
</BulletedList>

<BulletedList id="6A9kfE0cVbALQGaAmwxLlc">
  K8s: `livenessProbe` / `readinessProbe` 指向上述端点。
</BulletedList>

<Heading id="MK3P5dnM3A3CJSGqicjKD4" level="3">
  8.6 配置与密钥管理
</Heading>

<Table id="vEIJ2K8NgM8xxfmlIfu7VG" readonly rowHeader>
  <TableRow id="vgujvVCUeXpkq5VcGp1qYJ">
    <TableCell id="DtOxthZCi8J1boJ895bKFR">
      <Paragraph id="FopFjdBNqRywPpDKcrXXNE">
        配置层
      </Paragraph>
    </TableCell>

    <TableCell id="4UHWjPGygojfGx5SSWWsTV">
      <Paragraph id="ZhrzZehtXUMQmVuQ1ww2yj">
        方案
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="lsQz5JsSB2Ahh6hS57XsaY">
    <TableCell id="couwmjIQ4ev3atZAt1UmCb">
      <Paragraph id="48IScO1STBqboXH36QEps2">
        环境变量
      </Paragraph>
    </TableCell>

    <TableCell id="mHVseTFAECR4XHNxQAHIZF">
      <Paragraph id="SwHOwINK83YMpiQQTU5dsd">
        `.env` 文件（开发）/ Docker Compose `environment`（生产）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="SRbjb4QeaBNpGbbObdhaCi">
    <TableCell id="GOorLQAAhTCkhIm43zvN6S">
      <Paragraph id="ruUBy5f4EvZmolXGW9SmLi">
        敏感密钥
      </Paragraph>
    </TableCell>

    <TableCell id="EmOaylreWBRiCs5z8QmjbM">
      <Paragraph id="GHVNikDDXr1fik1TVicKiH">
        SOPS + age 加密 `.env.sops` → 运行时解密注入
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="Jbr15JX5G1dTYrzgn8fTjD">
    <TableCell id="Z7HlhlswawyucBHpkUeT9a">
      <Paragraph id="aDjB2lMSZcPatTMBci6xtz">
        高德 API key
      </Paragraph>
    </TableCell>

    <TableCell id="M7zydJdX28a6JRd8JP8gfo">
      <Paragraph id="dgQz4AjxjVmph0xhX9zq0E">
        存 `system_map_config.china_api_key_enc`（应用层 AES 加密）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="TqAqEa1PVaJOApQ3EeA2de">
    <TableCell id="77oSnnCkfaWZXi0l93ZTGq">
      <Paragraph id="q8Si9ZBqydfQTHOYapPj18">
        JWT 签名密钥
      </Paragraph>
    </TableCell>

    <TableCell id="SIyKOTl8HWtK99whnnfFe3">
      <Paragraph id="0Aj1IjFbE2K6iUBthSj7l0">
        环境变量 `JWT_SECRET`，轮换周期 90 天
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="wWX2YwF1wEisMNnyLIvat3">
    <TableCell id="uYr0i0UndIzoBWP7ovc0Qs">
      <Paragraph id="p64kLPb4S8pEmFWWp88LCk">
        agent\_token
      </Paragraph>
    </TableCell>

    <TableCell id="jHLDrr21YTUo6l80ujYczF">
      <Paragraph id="lVFPj7AAGPV57g735LOFWE">
        `compute_nodes.agent_token`，管理员触发轮换
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Heading id="vhuJxJb6EEEZZNVjOrb4iS" level="3">
  8.7 CDN 与边缘缓存
</Heading>

<BulletedList id="Z37ZMgdP9xherOhNSVnNS1">
  <Mark bold>HLS 分片</Mark>：`.ts` 分片设 `Cache-Control: public, max-age=86400`（24h），`.m3u8` 设 `max-age=10`（短缓存，允许档位切换）。
</BulletedList>

<BulletedList id="F8CgmTIMJwHlF2A2mE2uHi">
  <Mark bold>缩略图</Mark>：WebP 缩略图设 `max-age=2592000`（30天），内容变更有 hash 后缀做 bust。
</BulletedList>

<BulletedList id="DK3aOvBZaKbXUFPg0qFfaX">
  <Mark bold>CDN 策略</Mark>：Caddy 内置文件服务器 + `Cache-Control`；规模化后接 CloudFront/阿里CDN。
</BulletedList>

<BulletedList id="HMPVCH95BMtRioiLvpnqtb">
  <Mark bold>预热</Mark>：新索引完成后主动 `PREFETCH` 对应缩略图到 CDN。
</BulletedList>

<Heading id="esvRlbhC3zJrUUcoZXDF8q" level="3">
  8.8 数据迁移工具（从群晖 Photos 迁移）
</Heading>

<Code id="YquAl3vPrktuB9Sms2EWgW">
  ```
  DS1819+ Synology Photos 导出
    → 扫描 /volume1/photo/ + /volume1/photo/@eaDir/（缩略图/sidecar）
    → 保留 EXIF + 原始目录结构（folder_path 映射）
    → 读取 Synology sidecar（.info, .extoolkit）提取人物/标签/收藏
    → 映射到本系统 media/people/tags/albums
    → 增量同步：rsync + inotify 监听新文件
    → 进度可视化：迁移进度条 + 冲突处理（重复/路径冲突）
  ```
</Code>

<BulletedList id="e0r2gVYnSsWtlsK2kqIFWC">
  迁移期间双系统并行运行（群晖只读，本系统增量写入）。
</BulletedList>

<Divider id="Q2mAQeBp2TGgemcElGYECL" />

<Heading id="RRBZYQEt6Oxx7g38UOr3zV" level="2">
  9. 性能与伸缩
</Heading>

<BulletedList id="0Ex7PEK0bcZK5yDZzTDoIA">
  虚拟滚动 + CDN 缩略图 → 10 万媒体首屏 \< 2s。
</BulletedList>

<BulletedList id="aVHxjJngkDm7vE9zyvhMYq">
  PG `tsvector` + pgvector IVFFlat 索引 → 搜索 \< 1s。
</BulletedList>

<BulletedList id="1SRPEXq43ceSzCyCrJSmcd">
  转码/AI 经队列横向扩展（GPU 节点可增删）。
</BulletedList>

<BulletedList id="8eGgbfdbwjbMSvoD1dAKNT">
  HLS 边缘缓存 → 4K 360 起播 \< 3s。
</BulletedList>

<Divider id="GBYvU4LphH0h5j6AgeKCqL" />

<Heading id="4vEIUVqMUei2M4Cd8BMF1k" level="2">
  10. 风险与对策
</Heading>

<Table id="ffDWGOdWu4AdQhDYrEms22" readonly rowHeader>
  <TableRow id="70qMzqJztvTr8m2SFddgeX">
    <TableCell id="QLNw9omO7AqfN30lmwS6tN">
      <Paragraph id="mdy8ON0gUjJgZmpiYEtXgN">
        风险
      </Paragraph>
    </TableCell>

    <TableCell id="P9CxTbE3vO8mINvglT7UoH">
      <Paragraph id="7vlQ5rz5j5jDACQdfSZfGQ">
        对策
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="hH2DSttrmYKatqyXCAVnyj">
    <TableCell id="T9jrfNhmChUKsBcxWHVkGO">
      <Paragraph id="5nRhy3avD2LxMTr9WK60Wi">
        NAS 无 GPU
      </Paragraph>
    </TableCell>

    <TableCell id="EGi1Klwg8G06t9Yiq99xhS">
      <Paragraph id="9MyZeR5WV7KbJWM7C3rrgn">
        算力分离（§6）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="l8DcU0ZXOyPZF2nSRGNdSd">
    <TableCell id="Ryx0hxdpsK7FEL1DwujIdL">
      <Paragraph id="a0jBVgsgTUsZ7EplIhjrvd">
        微信/WebXR 强制 HTTPS
      </Paragraph>
    </TableCell>

    <TableCell id="dUiVlV12ZiMdZNdw6P2Df2">
      <Paragraph id="UmL3cAnsiNmdBDEdb8npdN">
        Caddy 证书 + 公网域名（支持非标端口/端口转发/IP 直连）
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="7adIuMN5LzzXi2zGdIzAek">
    <TableCell id="rKOnMvVTC1Zj8FWppZOCsV">
      <Paragraph id="ZilhoRx5iJ2dwvs7NqwX70">
        大库索引耗时
      </Paragraph>
    </TableCell>

    <TableCell id="Li70ztqkY2nTwFEWGK7YWb">
      <Paragraph id="djoWFbbxaptuKKq5N3SO7E">
        增量 + 低峰 + 进度可视化
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="965BnctuQLBA0sDlYW8lQG">
    <TableCell id="laN8OvoaF9Q9JahxQBkrLD">
      <Paragraph id="nMu0dOq8yWZB08xO98hLBN">
        360 元数据缺失
      </Paragraph>
    </TableCell>

    <TableCell id="rBAQz3eqcHlEbthLsny82t">
      <Paragraph id="gFEhX4Qh4pFfzEz7vykH8d">
        导入补全 equirectangular
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="vZINh1J0FH7AzCsyspywsV">
    <TableCell id="yArtDLXJ64w1IkdpKnJpRb">
      <Paragraph id="L4uqn5eYi27tPn9fkTFkHD">
        SMB 写入与索引冲突
      </Paragraph>
    </TableCell>

    <TableCell id="dpeoqeBVJkud5CRWw6X0mw">
      <Paragraph id="5hLAYK3UZpzxuofb4iKaDD">
        监听去重 + 入队限速
      </Paragraph>
    </TableCell>
  </TableRow>

  <TableRow id="pgCKmvLgbjB8MLNewV5uz0">
    <TableCell id="PtU32fGpFcw2IB53119b8E">
      <Paragraph id="jrG69vU7LCOAKoOrD6PnBV">
        维护成本
      </Paragraph>
    </TableCell>

    <TableCell id="MFqU45NEqA1cJpGnEslT9L">
      <Paragraph id="GsPX09ru8MM34ONHW4VfUH">
        仅 360 引擎自研，其余拼装成熟库
      </Paragraph>
    </TableCell>
  </TableRow>
</Table>

<Divider id="hrjOf7kVVRwCQDV5Z9lwPE" />

<Heading id="RVcqfwMosj5w1iweS0zx8S" level="2">
  11. 技术选型与版本
</Heading>

<BulletedList id="siFKhXuNY3m0uOCie4lomi">
  后端：Go 1.22+（API/媒体）；Python 3.13（FastAPI AI）
</BulletedList>

<BulletedList id="hx9swYoPUEIOoRkxU4Z8JT">
  存储：PostgreSQL 16 + pgvector 0.7；MinIO/S3 对象存储
</BulletedList>

<BulletedList id="TPW4BbdlQRdIqzch59cGCo">
  前端：Vue 3 + Vite；MapLibre GL JS；Three.js / A-Frame；hls.js
</BulletedList>

<BulletedList id="WV3cV0WlEPtY53c46dq56x">
  转码：ffmpeg 6（subprocess 调用，不链接 libav\*）+ NVENC；BullMQ + <Mark bold>Valkey</Mark>（Redis 的 BSD 许可 fork，避免 RSALv2/SSPL 商业化风险）
</BulletedList>

<BulletedList id="atsvD7rEVRKqIexPHYbr0I">
  部署：Docker Compose → K8s；Caddy 反代
</BulletedList>

<BulletedList id="lcVmGAWsDMEgoUDhdcydNS">
  鉴权：Keycloak（OIDC）/ Authelia；otplib（2FA）
</BulletedList>

<Divider id="GwAmxuOnHu0YltUgKju0z5" />

<Paragraph id="XnWoYt9xCvgrko7Yfv4Eyp">
  <Mark italic>文档结束（TDD v1.1）。详细表结构与接口分别见《数据库 DDL.md》《API 详细契约.md》。</Mark>
</Paragraph>
