# Panomint v1.9.1 Release Notes

| 项 | 内容 |
| --- | --- |
| 版本 | **v1.9.1**（修订号 +1：全站视觉升级） |
| 发布日期 | 2026-10-02 |
| 数据库迁移 | 无 |
| 源分支 | feature/ui-morandi（2 commits） |

---

## 全站视觉升级：「晨雾 Morandi」（用户选定方案 H）

纯样式层升级（design tokens + 字体），**功能与数据零变化**：

- **色彩**：暖灰底 `#E9E4DE`、卡片 `#F4F1ED`、深雾蓝主色 `#4A5A6A`；全站无纯黑纯白，语义色莫兰迪化（陶土红/灰绿）
- **排版**：Outfit + Noto Sans SC 字体，全站字重 300 主导，层级靠字距与明度
- **形状**：圆角 6/10/14，阴影改双层漫射（近影+远影），去描边强度依赖
- **AI 助手面板**：主题对齐雾蓝，移除默认紫粉光效（视觉规范对齐）
- **时间轴图例**：四类别色莫兰迪化（颜色即数据语义不变）

## 部署指引

```bash
docker pull warlocks/panomint-web:1.9.1     # 本版仅 web（dist）与 app（版本串）变更
docker pull warlocks/panomint-app:1.9.1
# worker 内容同 1.9.0（tag 递增）；db 零变化
```

## 镜像 digest（2026-10-02 Hub 实测，115 出口独立查询，1.9.1 = latest 逐字一致）

| 镜像 | 1.9.1 = latest digest |
| --- | --- |
| warlocks/panomint-web | `sha256:9bafc7f5ceedd…`（Morandi dist + agent 前缀） |
| warlocks/panomint-app | `sha256:d5e952a862434…`（版本串 1.9.1） |
| warlocks/panomint-worker | `sha256:716c895a24235…`（同 1.9.0 内容） |
| warlocks/panomint-db | `sha256:fdc2262f9d420…`（同 1.9.0 内容） |
