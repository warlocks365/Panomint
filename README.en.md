# Panomint 360° Photo Album

**English** | [中文](README.md)

> A self-hosted 360° panoramic smart photo album — a modern alternative to Synology Photos

![Version](https://img.shields.io/badge/version-1.7.1-blue)
![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20Docker%20%7C%20NAS-lightgrey)
![Backend](https://img.shields.io/badge/backend-Go%201.26-00ADD8)
![Frontend](https://img.shields.io/badge/frontend-Vue%203-42B883)

## Overview

Panomint is a **fully self-developed, self-hosted** photo album system built around **360° panoramic videos and photos**: spherical playback, gyroscope and VR headset tracking, server-side HLS transcoding with adaptive bitrate, WeChat H5 sharing, timeline and full-screen map, AI-powered semantic search and automatic tagging.

- **Media-source agnostic**: local directories, SMB, NFS, or remote mounts all work as photo sources
- **Deployment agnostic**: any mainstream Linux, Docker Compose, NAS (ready-made compose for Synology DSM Container Manager), or a bare-metal server (binary + systemd) without containers
- **Storage/compute separation**: storage and compute nodes are decoupled; falls back to CPU automatically when no NVIDIA GPU is present
- **No vendor lock-in**: Synology Photos is the feature benchmark, but deployment never depends on DSM or any specific hardware

## Highlights

- **360° spherical playback**: Three.js sphere rendering + WebXR stereo mode; phone gyroscope works out of the box
- **Instant playback of GB-scale videos**: untranscoded large videos stream via HTTP Range requests (no transcoding wait); automatically hot-switches to multi-bitrate HLS once transcoding completes
- **HEVC compatibility hints**: H.265 footage from action cameras gets a clear guidance message instead of a silent failure on unsupported browsers
- **AI semantic search**: Chinese-CLIP vector retrieval — find photos with natural language (e.g. "sunset at the beach")
- **Auto tagging & face clustering**: YuNet detection + SFace embeddings, fully on-device inference with zero cloud dependency
- **Full-screen interactive map**: MapLibre GL + PostGIS geo-aggregation, photos plotted by GPS
- **Secure accounts**: JWT + optional TOTP 2FA, login lockout, password strength policy, invite-code registration, app passwords (for WebDAV), all managed in the admin console
- **Ops friendly**: backup/restore scripts, offline asset fetcher, health-check script, and a remote transcoding debug channel (time-boxed grant + one-time key)

## Feature List

**Media management**: photo/video upload & import, directory scanning (incl. NAS mounts), timeline (year/month/day), virtual folders, duplicate detection (pHash)

**Playback & sharing**: 360° spherical playback (drag/gyroscope/VR), HLS adaptive bitrate, Range-streamed fallback for untranscoded videos, HEVC codec detection hints, WeChat H5 share links, public share & download, EXIF display

**Intelligence**: Chinese semantic search (CLIP), auto tagging, face detection & clustering, GPS reverse geocoding (AMap, optional), full-screen map

**Accounts & security**: registration toggle + one-time invite codes, password strength policy, login lockout, admin password reset (one-time temp password + forced change), TOTP 2FA, app passwords, RBAC roles, audit log

**Admin console**: user & role management, account policies, scan-root assignment, transcoding & HLS settings, HTTPS certificate & port configuration, remote debug channel, version info

**Deployment & ops**: Docker Compose (generic/Synology templates + interactive generator), bare-metal server (systemd), three release package forms, backup/restore, offline asset fetch

## Architecture

| Layer | Technology | Notes |
|-------|-----------|-------|
| Backend | Go 1.26 (gin + pgx + goose) | Single binary covering API/index/transcode/auth/AI |
| AI inference | Go + CGO + ONNX Runtime | Chinese-CLIP (default) / OpenAI CLIP, YuNet + SFace faces; auto-degrades without CGO |
| Database | PostgreSQL 16 + PostGIS 3 + pgvector | Geo aggregation + vector search in one store |
| Frontend | Vue 3 + Vite + Pinia | + MapLibre GL + Three.js + hls.js + vue-virtual-scroller |
| Transcoding | ffmpeg (subprocess, no libav\* linkage) | HLS multi-bitrate; optional NVENC, auto CPU fallback |
| Queue | Self-built (BullMQ-compatible semantics) | On Valkey: waiting/delayed/processing/failed |
| Deployment | Docker Compose / systemd | Reverse proxy: built-in nginx (web container) or optional Caddy TLS |

**Key design decisions**: media ingestion is restricted to three controlled paths (HTTP upload / scan import / storage reconcile); visibility is enforced by a single source-of-truth module (mediascope) that every user-facing media query must pass through; the task queue is self-implemented with no external message broker; all AI model assets are fetched explicitly via scripts for offline-friendly deployment.

## Quick Start

### Docker Compose (recommended)

```bash
git clone https://github.com/warlocks365/Panomint.git
cd Panomint
bash scripts/generate-compose.sh   # interactive generator (supports Synology mode)
docker compose up -d
```

Visit `http://<host>:8088` for the first-run wizard (create the admin account).

Fetch AI models and static assets explicitly (online or offline package):

```bash
bash scripts/fetch-all-assets.sh
```

### Synology DSM

Import `release/docker/docker-compose.synology.yml` in Container Manager, or run the generator in Synology mode.

### Bare-metal server (no containers)

Binary + systemd: see the [standalone deployment guide](文档/独立部署指南_v1.0.md) (Chinese) and `deploy/systemd/`.

### Requirements

| Tier | Notes |
|------|-------|
| Minimum | 2 vCPU / 4GB RAM / PostgreSQL 16; AI runs on CPU |
| Recommended | 4 vCPU / 8GB+ RAM; optional NVIDIA GPU for NVENC transcoding |

## Documentation

Documents are written in Chinese; see the [Chinese README](README.md) for the full list — highlights: [User Manual](文档/用户操作手册_v1.0.0.md), [API Contract v1.2](文档/API详细契约_v1.2.md), [Database DDL v1.1](文档/数据库DDL_v1.1.md), [TDD v1.1](文档/技术设计文档_TDD_v1.1.md), [PRD v3.1](文档/相册系统详细需求文档_PRD_v3.1.md), [Release Management](文档/版本管理规范.md), [Standalone Deployment](文档/独立部署指南_v1.0.md).

Release Notes: [v1.0.0](文档/Release_Notes_v1.0.0.md) · [v1.1.0](文档/Release_Notes_v1.1.0.md) · [v1.2.0](文档/Release_Notes_v1.2.0.md) · [v1.3.0](文档/Release_Notes_v1.3.0.md) · [v1.4.0](文档/Release_Notes_v1.4.0.md) · [v1.5.0](文档/Release_Notes_v1.5.0.md) · [v1.6.0](文档/Release_Notes_v1.6.0.md) · [v1.7.0](文档/Release_Notes_v1.7.0.md) · [v1.7.1](文档/Release_Notes_v1.7.1.md)

## Changelog

### v1.7.1 (2026-09-27)

- Fallback playback of large files now uses HTTP Range streaming: GB-scale untranscoded videos start instantly, seeking is precise, and browser memory no longer grows with file size
- HEVC videos get an explicit guidance message on browsers without decoding support (suggest enabling HLS transcoding) instead of a generic load failure
- Playback error messages now include the HTTP status code

### v1.7.0 (2026-09-26)

- New admin console "Account Policies" tab: registration toggle & invite codes, password strength, login lockout — all in one place
- Self-service registration with one-time invite codes (disabled by default)
- Login lockout: default 5 failed attempts locks the account for 15 minutes; a successful login resets the counter
- Password strength policy: minimum length and character-class requirements (default 8 chars)
- Admin password reset now issues a one-time temporary password shown once, with forced change at next login
- Disabling an account immediately revokes all of its sessions; user list shows lock & forced-change status inline
- Configurable HTTP/HTTPS ports: redirects follow the HTTPS port and take effect immediately

### v1.6.0 (2026-09-26)

- Fallback playback for untranscoded 360° videos: drag/gyroscope/VR all preserved without HLS, auto-switches to multi-bitrate streams after transcoding
- New fallback status bar with four states (transcoding / failed / not-transcoded / gate closed)
- Fixed a template branch mis-pairing that rendered an empty video player on some states

### v1.5.0 (2026-09-26)

- HLS streaming settings in the admin console (switch & parameters)
- HTTPS/certificate settings and access-guard hardening
- User manual updated

### v1.4.0 (2026-09-26)

- Remote transcoding debug channel: admin grants a time-boxed authorization (TTL presets, one-time credentials, two-step confirm); engineers diagnose via encrypted WebSocket
- debugctl CLI tool (snapshot collection / session diagnostics, credential-file hygiene)
- CI optimization: docs-only pushes no longer trigger full builds

### v1.3.0 (2026-09-26)

- Play-time auto-transcode switch: start transcoding instantly on playback (AND-combined with the system-level gate)
- Chinese localization of index status labels in the admin overview

### v1.2.0 (2026-09-25)

- Enhanced admin scan import: directory-tree picker with lock hints for unauthorized paths
- Per-account scan-root assignment; members can self-trigger scans
- Version info card in Settings (current version + history)
- In-app user manual (usage + deep-dive two-level content)
- Auto-HLS-transcode switch promoted to system level; new videos play as original files when off
- Removed a dead "storage location" feature; mount-import landing semantics (landing_dir)

### v1.1.0 (2026-09-25)

- Admin scan import: batch ingestion from a chosen directory with optional auto-scan on start
- Fixed copy-overwrite bug where a copied duplicate could overwrite its source
- Fixed a cross-tab display issue on the storage tab

### v1.0.1 (2026-09-24)

- Fixed the web image bundling a stale frontend that hid the first-run wizard

### v1.0.0 (2026-09-24)

- First public release: 360° panorama/gyroscope/VR tracking, HLS streaming, timeline, full-screen map, AI semantic search & tagging, face clustering, albums & sharing, admin console

## Repository Layout

```
Panomint/
├── docker-compose.yml          # Orchestration (PG/PostGIS/pgvector + Valkey + api/web/worker + caddy; MinIO optional profile)
├── docker/                     # Per-service Dockerfiles (db/web/worker/api) and Caddy config
├── release/                    # Three release forms (Docker/bare-metal/bundle) + Synology compose template
├── scripts/                    # compose generator / asset fetch / backup-restore / health checks / quality gates
├── deploy/systemd/             # systemd unit templates for bare-metal deployment
├── src/
│   ├── backend/                # Go backend (cmd/ entrypoints + internal/ packages + migrations/)
│   └── frontend/               # Vue 3 + Vite frontend
├── 文档/                       # Product docs in Chinese (manual/API contract/DDL/TDD/PRD/release process/Release Notes)
└── licenses.csv                # Third-party dependency licenses
```

## Development

```bash
# Backend
cd src/backend && go build ./... && go test ./...
# Frontend
cd src/frontend && npm ci && npm run build
# Database migrations (auto-run on start; to build the tool manually:)
go build ./cmd/migrate
```

Quality gates: CI (`.github/workflows/ci.yml`), frontend org-size ratchet (`scripts/frontend_org_guard.py`), P0 security guards (`scripts/p0_guard.py`).

## License

The source is made public for learning, evaluation, and self-hosting purposes; redistribution rights are not granted except where stated in the repository. Third-party dependency licenses are listed in [licenses.csv](licenses.csv).
