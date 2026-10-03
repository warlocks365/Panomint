package mediascope

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ---- 「别忘带可见性谓词」的 repo 级源码形状守卫 ----
//
// 背景：2026-09-17 发现 6 处媒体可见性缺失，其中 4 处正在跨用户泄漏（已修复并真机复验）。
// 复盘出的根因**不是**"少写了一个 if"，而是：
//   - 可见性谓词的唯一真源是 internal/mediascope，但它只被 2 个包 import，
//     其余各处**各自手写或干脆不写**；
//   - "列清单"有 repo 级守卫（internal/media/mediaref_single_source_test.go，遍历
//     internal/ + cmd/），**可见性谓词却没有任何同等扫描**。
// 于是"写一条新的媒体查询 → 忘带可见性"在**编译期与测试期都拦不住**，全靠"记得调用"。
// 真实先例：internal/media/thumb.go 与 internal/media/tags.go 曾是与 scopeConds 并存的
// **零属主条件**查询，而 internal/media/scope_test.go 当时**全绿**。
//
// 本文件补的就是那个机制：按**SQL 文本特征**（不是函数名）在源码里找"接触 media 表的查询"。
//
// 为什么不按函数名找：本项目已踩过一次 —— 按 `ListMediaByTag` 之类的函数名 grep 去统计调用点，
// 结果漏掉了两个真实调用点（名字对不上）。漂移的形态是"有人又写了一条 media 查询"，
// 而**写查询**这个动作必然留下 `FROM media` / `JOIN media` / `UPDATE media` 这类 SQL 文本，
// 函数叫什么都不影响它出现。所以扫描的锚点是 SQL 文本，不是标识符。
//
// 为什么不按"是否出现 mediascope."来找：那只能找出"已经接了但可能接错"的，
// 找不出"**根本没接**"的 —— 而后者才是事故来源（三条泄漏查询里没有一条调用过 mediascope）。
//
// 三道守卫：
//   - 守卫 A（TestMediaVisibility_UnregisteredMediaQueryFails）：新出现的 media 查询必须在
//     登记表里，否则 FAIL，失败信息直接给出 `文件:行号 + 命中的特征`。防"新文件没登记"。
//   - 守卫 B（TestMediaVisibility_UserFacingFilesAreWired）：登记为"面向终端用户"的文件，
//     必须**真的**接上了可见性判定（mediascope 调用、具名 handler 层判定、或内联 owner 绑定谓词）。
//     防"登记了但忘了接"。
//   - 守卫 C（TestMediaVisibility_GuardIsNotVacuous）：把"未登记的新文件"喂给同一套判定，
//     必须命中；把某条登记摘掉，那个真实文件必须立刻变成未登记。防"永远通过的假守卫"。
//
// 登记表的判断准则（写新条目时照做）只有一个问题：
//
//	**这条查询会不会把 media 行 / media 内容返回给终端用户？**
//
//   - 会 → 必须有可见性或归属判定，并写清判定在哪个文件哪个函数（守卫 B 会去核）；
//   - 不会（后台 worker 全库扫描、写路径、按 id 的归属判定原语本身、统计计数……）→
//     登记为**有意为之**并写清为什么不会泄漏；
//   - **不确定 → 标"待确认"，不要猜**。本项目已多次因"自造前提"翻车，
//     一个看起来合理但错误的理由，比一个空着的条目更危险。
//
// 粒度说明：登记键是（文件, 特征）。同一个文件同一特征可能既有面向用户的语句、也有内部的
// 语句（如 media/write.go 的 `FROM media` 同时出现在 ownerOf 原语与 ListTrash 里）。
// 此时**取更严的一档**（UserFacing=true），并在理由里分别说明每处的性质。

// mediaSQLFeature 是"接触 media 表的 SQL"的一个文本特征。
//
// 顺序即优先级：位置靠前的特征更具体（DELETE FROM media 是 FROM media 的特例），
// 失败信息里优先报更具体的那个动词。
type mediaSQLFeature struct {
	name string
	re   *regexp.Regexp
}

// mediaSQLFeatures 返回全部特征。大小写不敏感；`\s+` 容忍换行与多余空白。
//
// `media\b` 的边界很重要：它让 `FROM media_tags` / `FROM media_refs` 这类**别的表**
// 不被误判（`_` 是词字符，故 `media\b` 在 `media_tags` 上不匹配）。
func mediaSQLFeatures() []mediaSQLFeature {
	return []mediaSQLFeature{
		{"DELETE FROM media", regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+media\b`)},
		{"INSERT INTO media", regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+media\b`)},
		{"UPDATE media", regexp.MustCompile(`(?i)\bUPDATE\s+media\b`)},
		{"JOIN media", regexp.MustCompile(`(?i)\bJOIN\s+media\b`)},
		{"FROM media", regexp.MustCompile(`(?i)\bFROM\s+media\b`)},
	}
}

// mediaQueryHit 一次命中：文件 + 行号 + 特征。
type mediaQueryHit struct {
	File    string // 相对 src/backend，统一 `/` 分隔
	Line    int
	Feature string
	Line1   string // 命中所在行（去掉首尾空白，供失败信息直接展示）
}

// backendRoot 从 internal/mediascope 回到 src/backend。
func backendRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("解析后端根目录失败: %v", err)
	}
	return root
}

// scanMediaQueries 遍历 root 下的 internal/ 与 cmd/，找出所有接触 media 表的 SQL 命中。
//
// 与 internal/media/mediaref_single_source_test.go 同骨架：WalkDir + 跳过 testdata/隐藏目录 +
// **排除 _test.go**（测试里可以随便写 SQL 字面量，例如本文件守卫 C 的变异样本，
// 以及 internal/media/mediaref_single_source_test.go 里逐字抄下来的旧副本）。
func scanMediaQueries(root string) ([]mediaQueryHit, error) {
	features := mediaSQLFeatures()
	var hits []mediaQueryHit
	for _, sub := range []string{"internal", "cmd"} {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			continue // cmd/ 在某些检出形态里可能不存在
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if name == "testdata" || name == "node_modules" || name == "vendor" || strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			src := string(b)
			for _, f := range features {
				for _, loc := range f.re.FindAllStringIndex(src, -1) {
					hits = append(hits, mediaQueryHit{
						File:    rel,
						Line:    strings.Count(src[:loc[0]], "\n") + 1,
						Feature: f.name,
						Line1:   firstLine(src[loc[0]:]),
					})
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		if hits[i].Line != hits[j].Line {
			return hits[i].Line < hits[j].Line
		}
		return hits[i].Feature < hits[j].Feature
	})
	return hits, nil
}

// firstLine 取从命中位置起到该行结尾的一段文本（压缩空白、截断），供失败信息阅读。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 140 {
		s = s[:140] + "…"
	}
	return s
}

// unregisteredGroup 一组未登记的命中（同一 文件+特征 的全部行号）。
type unregisteredGroup struct {
	File    string
	Feature string
	Lines   []int
	Line1   string
}

// findUnregistered 用给定的登记表扫描 root，返回未登记的命中组与总命中数。
//
// 抽成函数是为了让守卫 C 能用**同一套判定**分别对"合成的新文件"和"被摘掉登记项的真实仓库"
// 做变异测试 —— 若守卫 C 另走一套逻辑，它证明的就只是那套逻辑，不是守卫本身。
func findUnregistered(root string, reg map[string]mediaQueryRegistration) ([]unregisteredGroup, int, error) {
	hits, err := scanMediaQueries(root)
	if err != nil {
		return nil, 0, err
	}
	var groups []unregisteredGroup
	idx := map[string]int{}
	for _, h := range hits {
		k := regKey(h.File, h.Feature)
		if _, ok := reg[k]; ok {
			continue
		}
		if i, ok := idx[k]; ok {
			groups[i].Lines = append(groups[i].Lines, h.Line)
			continue
		}
		idx[k] = len(groups)
		groups = append(groups, unregisteredGroup{h.File, h.Feature, []int{h.Line}, h.Line1})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].File != groups[j].File {
			return groups[i].File < groups[j].File
		}
		return groups[i].Feature < groups[j].Feature
	})
	return groups, len(hits), nil
}

// regKey 登记表的键：文件 + 特征。
func regKey(file, feature string) string { return file + "\x00" + feature }

// mediaQueryRegistration 一条登记项。
type mediaQueryRegistration struct {
	// UserFacing：这条查询会不会把 media 行 / media 内容返回给终端用户。
	UserFacing bool
	// Reason：人类可读的理由。UserFacing=false 时必须写清"为什么不会泄漏"。
	Reason string
	// Evidence：依据（源码行号 / 调用链 / 端点）。
	Evidence string
}

// registeredMediaQueries 是**显式登记表**，键为 文件+特征，见 regKey。
//
// 准入规则见文件头。条目必须诚实：不确定的写成"待确认"并说明缺什么信息，
// 不要写一个看起来合理但没核实过的理由。
var registeredMediaQueries = map[string]mediaQueryRegistration{

	// ---- 后台 worker / CLI：全库扫描，行只进服务端内存，不回给终端用户 ----

	regKey("cmd/migrateplan/main.go", "FROM media"): {
		UserFacing: false,
		Reason:     "一次性迁移计划 CLI 的 loadExisting：读全库 hash/path/id 生成迁移计划并写本地 JSON，不经 HTTP、不面向终端用户。",
		Evidence:   "cmd/migrateplan/main.go:180-200（loadExisting），调用方为该命令的 main()。",
	},
	regKey("internal/phash/store.go", "FROM media"): {
		UserFacing: false,
		Reason:     "phashgen 后台 worker 取「已有 MD 缩略图且未算 phash」的候选行（:53）与统计计数（:110），只喂给 CLI 循环。",
		Evidence:   "phash/store.go:40-56 ListPending、:104-111 Stats；调用方 cmd/phashgen。",
	},
	regKey("internal/phash/store.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "worker 写 phash / phash_scanned_at 的回写路径，不返回 media 内容。",
		Evidence:   "phash/store.go:82、:91。",
	},
	regKey("internal/embed/store.go", "FROM media"): {
		UserFacing: false,
		Reason:     "embedgen 后台 worker 取待向量化候选（:57）、标定用批次（:106）与统计计数（:81），只喂给 CLI 循环。",
		Evidence:   "embed/store.go:45-75 ListPending、:99-131 ListEmbedded；调用方 cmd/embedgen。",
	},
	regKey("internal/embed/store.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "worker 写 embedding 的回写路径，不返回 media 内容。",
		Evidence:   "embed/store.go:91。",
	},
	regKey("internal/faces/store.go", "FROM media"): {
		UserFacing: false,
		Reason:     "facesgen 后台 worker 取「已有 LG 缩略图且未扫脸」的候选行（:48）与统计计数（:72），只喂给 CLI 循环。",
		Evidence:   "faces/store.go:36-51 ListPending；调用方 cmd/facesgen。",
	},
	regKey("internal/faces/store.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "worker 标记 faces_scanned_at 的回写路径，不返回 media 内容。",
		Evidence:   "faces/store.go:159。",
	},
	regKey("internal/index/index.go", "FROM media"): {
		UserFacing: false,
		Reason:     "按内容 hash 的全库查重（findByHash），返回值只用于设置 duplicate_of，不把该行的任何字段回给客户端。",
		Evidence:   "index/index.go:69-79。",
	},
	regKey("internal/index/index.go", "INSERT INTO media"): {
		UserFacing: false,
		Reason:     "入库写路径（insertMedia），owner_id 由调用方（登录会话）提供。",
		Evidence:   "index/index.go:81-110。",
	},
	regKey("internal/index/worker.go", "FROM media"): {
		UserFacing: false,
		Reason:     "缩略图 worker 读该行的 edits 以渲染，写路径/后台路径，行不回给用户。",
		Evidence:   "index/worker.go:42。",
	},
	regKey("internal/index/worker.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "worker 写 thumbnail_sm/md/lg 的回写路径。",
		Evidence:   "index/worker.go:103。",
	},
	regKey("internal/compute/store.go", "FROM media"): {
		UserFacing: false,
		Reason:     "控制端 worker 为已领取的转码任务补输入路径（pollInputPathsSQL），是节点侧调度数据，不面向终端用户。",
		Evidence:   "compute/store.go:130-132。",
	},
	regKey("internal/compute/store.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "worker 回写 hls_master（publishHLSMasterSQL，带 kind='hls' 与非删除守卫），是产物地址落库，不返回 media 内容。",
		Evidence:   "compute/store.go:154-165。",
	},
	regKey("internal/transcode/enqueue.go", "FROM media"): {
		UserFacing: false,
		Reason:     "批量入队（missingHLSQuery / EnqueueMissingHLS）：后台/CLI 扫「缺 HLS 的视频」，结果只用于排任务。",
		Evidence:   "transcode/enqueue.go:13-24、:33-41。",
	},
	regKey("internal/transcode/worker.go", "FROM media"): {
		UserFacing: false,
		Reason:     "转码 worker 取 path/宽高作 ffmpeg 输入，后台路径。",
		Evidence:   "transcode/worker.go:71。",
	},
	regKey("internal/transcode/worker.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "worker 回写 hls_master，后台路径。",
		Evidence:   "transcode/worker.go:103。",
	},
	regKey("internal/tags/store.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "taggen worker 标记 tags_scanned_at 的回写路径（MarkTagsScanned）。",
		Evidence:   "tags/store.go:222-227。",
	},
	regKey("internal/media/write.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "写路径回写（rating/notes/edits/软删/恢复），全部在 handler 先 checkAccess 之后才调用；不返回 media 内容。",
		Evidence:   "media/write.go:52/65/143/199/219；调用方 write_handlers.go（Favorite/Rating/Rotate/Delete/Restore）。",
	},
	// Job000143 媒体元数据编辑（拍摄时间/地点/详细地址/GPS）。
	// 判定准则「会不会把 media 行 / 内容返回给终端用户」= 不会：
	// 本语句只 UPDATE 四个元数据列、不带 RETURNING，响应体由 handler 用请求值回显
	// （已转 WGS-84 的坐标等），不读取任何 media 内容；
	// 调用前同样经过 h.checkAccess（write_handlers.go Patch 的元数据段），
	// 与既有 notes/edits 写路径同一条鉴权链。
	regKey("internal/media/metadata.go", "UPDATE media"): {
		UserFacing: false,
		Reason: "元数据写路径（taken_at/place/address/gps），仅 UPDATE 四列且无 RETURNING；" +
			"调用前已过 h.checkAccess（与 notes/edits 同一鉴权链），不返回 media 内容。",
		Evidence: "media/metadata.go SetMetadata；write_handlers.go Patch 的 Job000143 段。",
	},
	regKey("internal/media/write.go", "DELETE FROM media"): {
		UserFacing: false,
		Reason: "Purge 永久删除写路径，调用前已过 h.checkAccess（write_handlers.go Purge），只 RETURNING path 供清文件与审计。" +
			"P1-02：DELETE 前同事务显式 UPDATE 解除 albums/people 封面与 duplicate_of/live_photo_pair_id 引用（计数入审计）。",
		Evidence: "media/write.go Purge（事务内解引用 + DELETE ... RETURNING path）；write_handlers.go Purge。",
	},
	// Job000069 补登记（2026-09-22 中央终验逮到 Job000056/066 漏登 4 组）：
	regKey("internal/index/index.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "Job000056 @eaDir 复用回写：insertMedia 挂点同一条 UPDATE 落三档缩略图（成功即不入队），与 worker.go 缩略图回写同语义，行不回给终端用户。",
		Evidence:   "index/index.go:300-315。",
	},
	regKey("internal/media/batch_store.go", "FROM media"): {
		UserFacing: true,
		Reason:     "batchOwnedIDs 归属收敛查询：WHERE id=ANY($1) AND owner_id=$2 AND deleted_at IS NULL——批量操作入口收敛（Job000066），无权与不存在同形入 failed；返回只是 id 白名单。",
		Evidence:   "media/batch_store.go:16-19。",
	},
	regKey("internal/media/batch_store.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "批量软删/移动/设元：ids 全部先经 batchOwnedIDs 归属收敛（端点层一次收敛），此处只执行白名单化写；copySourceRow 的 FROM 同理（源 id 已过收敛）。",
		Evidence:   "media/batch_store.go:40-51、92-99、117-135、144-151。",
	},
	regKey("internal/media/batch_store.go", "INSERT INTO media"): {
		UserFacing: false,
		Reason:     "insertCopiedRow 批量复制写路径：owner_id 显式=调用者（$2），INSERT-SELECT 源行 id 已过 batchOwnedIDs 收敛。",
		Evidence:   "media/batch_store.go:130-146。",
	},
	regKey("internal/media/batch_store.go", "JOIN media"): {
		UserFacing: false,
		Reason:     "Job000100 batchAddToAlbumQueries 的写入前两段式语句（整笔可见性 count + INSERT INTO album_items）：ids 已过 batchOwnedIDs 归属收敛，JOIN 仅做写入前可见性整笔校验（跨属主 admin 场景防脏行），不回返媒体行/内容给终端用户。",
		Evidence:   "media/batch_store.go:211-233（batchAddToAlbumQueries）。",
	},
	regKey("internal/folders/manage.go", "FROM media"): {
		UserFacing: false,
		Reason:     "ownFolder 归属校验：只取 count(*) 与 count(*) FILTER (owner_id<>uid)，不返回媒体行/内容；谓词自带 owner 收敛语义（Job000069）。",
		Evidence:   "folders/manage.go:316-320。",
	},
	regKey("internal/folders/manage.go", "UPDATE media"): {
		UserFacing: false,
		Reason:     "目录改名/删除的批量组织变更：WHERE owner_id=uid 收敛前缀（Rename 改 folder_path、Delete 软删入回收站），与 WebDAV MOVE/RemoveAll 同语义（Job000069）。",
		Evidence:   "folders/manage.go:157-165、213-219。",
	},
	regKey("internal/media/upload.go", "INSERT INTO media"): {
		UserFacing: false,
		Reason:     "上传入库写路径，owner_id/space 由登录会话决定，不返回 media 行（只返回本次结果）。",
		Evidence:   "media/upload.go:393-410。",
	},

	// ---- WebDAV（Job000055，契约 §16）：Basic 认证后即"该用户的文件系统"，行为等价于 SMB 挂载 ----

	regKey("internal/media/dav.go", "FROM media"): {
		UserFacing: true, // 检出内容经 GET/PROPFIND 流给**已认证本人**——等价于下载自己的文件
		Reason: "WebDAV 读路径（listChildren/findByPath/folderExists/OpenFile）：可见性判定 = Basic 认证身份（fs.userID 来自认证中间件）" +
			"AND m.owner_id = $1 AND m.space='personal' AND m.deleted_at IS NULL，另叠加本包 ReadCond 双保险；" +
			"绝不返回他人媒体——owner 谓词写死在查询里，非可选。",
		Evidence: "media/dav.go: listChildren（ReadCond(3,…)）/findByPath（ReadCond(4,…)）/OpenFile；DAVHandler 的 Basic 认证在进 FS 之前完成。",
	},
	regKey("internal/media/dav.go", "UPDATE media"): {
		UserFacing: false,
		Reason: "WebDAV 写路径（RemoveAll 目录软删 / Rename 组织变更）：只回写 folder_path/filename/deleted_at，" +
			"不 SELECT 返回媒体内容；owner 限定 fs.userID（Basic 认证身份），跨用户写不可能。",
		Evidence: "media/dav.go: RemoveAll（UPDATE media SET deleted_at）/Rename（UPDATE media SET folder_path…）；文件级软删复用 Store.SoftDelete（已在 write.go 登记）。",
	},

	// ---- 归属判定原语 / 布尔判定：判定的输入端，本身不交付媒体内容 ----

	regKey("internal/media/write.go", "FROM media"): {
		UserFacing: true, // 取严：同特征还覆盖 ListTrash（面向用户），见 Reason 第 3 点
		Reason: "① ownerOf 是**写路径归属判定原语**（只取 owner_id/deleted_at，供 checkAccess 使用）；readAccessOf 是**读路径判定原语**" +
			"（P2-01，谓词出自 mediascope.ReadCond，供 checkReadAccess/readAllowed 使用）；② GetEdits 与 Purge(RETURNING path) 是写路径的前置读/删除，" +
			"handler 调用前已 checkAccess；③ ListTrash 直接面向终端用户（GET /media/trash），但 WHERE 内联 `m.owner_id = $1` 绑定调用者，" +
			"writer 侧把 c.GetString(\"user_id\") 传进来（write_handlers.go Trash），本就不返回他人媒体 —— " +
			"故未接 mediascope 是**有意为之**（其注释说明了为何不补 space='personal'：纯负收益）。",
		Evidence: "media/write.go ownerOf / readAccessOf / GetEdits / Purge / ListTrash；write_handlers.go Trash、checkAccess、checkReadAccess。",
	},
	regKey("internal/shares/store.go", "JOIN media"): {
		UserFacing: false,
		Reason: "MediaInShare 的 EXISTS：回答「这条媒体是否属于该分享目标」的布尔判定（缩略图/HLS 越权防护），不返回媒体行。" +
			"⚠️ 2026-09-18 补：仅判 album_items+deleted_at 会留下「列表已过滤、仍能按 media id 换到匿名缩略图字节」的侧门" +
			"（真机复现：匿名 GET .../thumb 200 + 40716 字节），故 EXISTS 里也按**相册属主**的可见集过滤" +
			"（mediascope.VisibleCondFor(3, albumOwner, \"m\")）。",
		Evidence: "shares/store.go MediaInShare（SELECT type, owner_id FROM albums + VisibleCondFor(3, albumOwner, \"m\")）。",
	},
	regKey("internal/transcode/transcode.go", "FROM media"): {
		UserFacing: false,
		Reason:     "两处都是**归属判定原语**：CreateJob 取 owner_id/type 后 canAccessMedia（:139）；ServeHLS 取 owner_id 后 canAccessMedia（:261）。只读属主列，不交付媒体内容。",
		Evidence:   "transcode/transcode.go:126-142、:250-264。",
	},
	regKey("internal/mediascope/scope.go", "FROM media"): {
		UserFacing: false,
		Reason: "**命中来自注释，不是 SQL**：:113-116 是解释 `qual()` 为何需要 alias 参数的包内文档，" +
			"举了 internal/geo 自带 `FROM media` 裸列名查询为例。本文件（可见性唯一真源）自身不含任何 media 查询。",
		Evidence: "mediascope/scope.go:111-116（注释文本）。",
	},

	// ---- 面向终端用户：必须有可见性/归属判定（守卫 B 会逐条去核接线） ----

	regKey("internal/media/timeline.go", "FROM media"): {
		UserFacing: true,
		Reason:     "GET /media 主列表，返回 media 行；作用域谓词取自 scopeConds（真源 mediascope.Conds）。",
		Evidence:   "media/timeline.go:107-110；handler media/handlers.go:58-59 ResolveMediaScope。",
	},
	regKey("internal/media/duplicates.go", "FROM media"): {
		UserFacing: true,
		Reason:     "GET /media/duplicates，返回重复组与媒体 ID；作用域谓词取自 scopeConds。",
		Evidence:   "media/duplicates.go:249-250；handler media/handlers.go:107-113。",
	},
	regKey("internal/media/histogram.go", "FROM media"): {
		UserFacing: true,
		Reason:     "GET /media/date-histogram，泄漏过他人媒体的按日计数；作用域谓词取自 scopeConds。",
		Evidence:   "media/histogram.go:34-41；handler media/handlers.go:135-141。",
	},
	regKey("internal/media/tags.go", "FROM media"): {
		UserFacing: true,
		Reason: "GET /tags/:id/media（ListMediaByTag）。tags 表全局无 owner，故必须按 media 可见性过滤；" +
			"曾有「只有 deleted_at + tag_id + confirmed、无任何可见性条件」的越权，修复即复用 scopeConds。",
		Evidence: "media/tags.go:310-343（:326 scopeConds）；handler media/tag_handlers.go:367-368 ResolveMediaScope。",
	},
	regKey("internal/media/thumb.go", "FROM media"): {
		UserFacing: true,
		Reason: "GET /media/:id/thumb 交付**二进制媒体内容**；曾是可绕过 GET /media/:id 的 IDOR 侧门（实测 viewer 拿到他人 200 + 7474 字节）。" +
			"现先判读访问再取列，无权与不存在同形 404。P2-01 后读口径扩为「属主 ∪ shared 成员 ∪ owner/admin」，" +
			"判定与 Detail/Download 共用 readAccessOf → mediascope.ReadCond（单一真源）。",
		Evidence: "media/thumb.go（h.thumbAccess 先于 SELECT；thumbAccessCheck → Store.readAllowed → readAccessOf）。",
	},
	// Job000131 补登记（2026-09-27，HEVC 兼容播放实时转码兜底端点）：
	regKey("internal/media/live_handlers.go", "FROM media"): {
		UserFacing: true,
		Reason: "GET /media/:id/live/master.m3u8 与 /media/:id/live/:name（实时转码兜底分片）——" +
			"交付**转码流**，两个端点调用前均先过 h.Media.checkReadAccess（与 Download 同判定，" +
			"无权 404/403 同形），且仅服务已授权媒体的转码产物，不返回 media 行数据本身。",
		Evidence: "media/live_handlers.go MasterM3U8/Segment（先 checkReadAccess → Manager.Ensure/Get）。",
	},
	regKey("internal/media/detail.go", "FROM media"): {
		UserFacing: true,
		Reason: "GET /media/:id 详情，返回 media 全字段；读访问由 handler 层 checkReadAccess 在调用 Store.GetDetail **之前**判定" +
			"（P2-01：口径扩为「属主 ∪ shared 成员 ∪ owner/admin」，与 /media?space=shared 列表同口径）。",
		Evidence: "media/detail.go GetDetail；write_handlers.go Detail（先 h.checkReadAccess → Store.readAccessOf）。",
	},
	regKey("internal/media/upload.go", "FROM media"): {
		UserFacing: true, // 取严：同特征覆盖 Download（面向用户）与上传去重（内部）
		Reason: "两处性质不同：① Download（GET /media/:id/download）交付**原文件流**，调用前已过 h.checkReadAccess" +
			"（P2-01 读口径：属主 ∪ shared 成员 ∪ owner/admin）；② 上传时的同人同 hash 去重，" +
			"WHERE 自带 `owner_id = $2`（= 上传者自己），不跨用户。",
		Evidence: "media/upload.go（Download + checkReadAccess；hash 去重 owner_id = meta.OwnerID，P2-03 已修吞错）。",
	},
	// Job000101 补登记（2026-09-23，GET /albums/groups 个人空间按相册分组）：
	regKey("internal/albums/groups.go", "FROM media"): {
		UserFacing: true,
		Reason: "GET /albums/groups 的未分组桶（ungroupedQueries）：把不在本人任何相册里的 personal 媒体行" +
			"返回给终端用户；可见性谓词 mediascope.VisibleCondFor(1, userID, \"m\")（调用者可见集），" +
			"「不属于本人相册」谓词同主体绑 $2=调用者 —— 与 /media?album=none 同口径 fail-closed。",
		Evidence: "albums/groups.go ungroupedQueries（:95 VisibleCondFor(1, userID, \"m\") + notIn 子查询 a.owner_id=$2）；handler groups.go Groups（user_id 来自会话）。",
	},
	regKey("internal/albums/groups.go", "JOIN media"): {
		UserFacing: true,
		Reason: "GET /albums/groups 的分组计数与每组前 N 项（groupCountsQueries/groupItemsQueries）：把相册成员媒体行" +
			"返回给终端用户；可见性谓词 mediascope.VisibleCondFor(2, userID, \"m\")（调用者可见集，与 List 聚合段同真源）；" +
			"候选相册本身已 fail-closed 在 listGroupAlbumsSQL（a.owner_id=$1 AND space='personal' AND type IN manual/favorites）。",
		Evidence: "albums/groups.go groupCountsQueries（:66 VisibleCondFor(2, userID, \"m\")）、groupItemsQueries（:77 同）。",
	},
	regKey("internal/albums/store.go", "FROM media"): {
		UserFacing: true,
		Reason: "面向终端用户：① List 的 smart 相册摘要计数与首图（buildCriteriaWhere 按相册属主）；" +
			"② List 的 normal 计数/首图/封面缩略图 —— 三者均按**相册属主**的可见集过滤" +
			"（mediascope.VisibleCondFor(2, r.ownerID, \"m\")）；③ addItemsQueries 的可见性校验语句。" +
			"List 只列**本人**相册（listAlbumsSQL `WHERE a.owner_id = $1`）。",
		Evidence: "albums/store.go listAlbumsSQL；List 的 normal 分支（VisibleCondFor(2, r.ownerID, \"m\")）；" +
			"addItemsQueries（VisibleCondFor(2, albumOwnerID, \"m\")）。",
	},
	regKey("internal/albums/store.go", "JOIN media"): {
		UserFacing: true,
		Reason: "面向终端用户：① List 的 normal 计数/首图；② Get 的 album_items 条目 —— 两者均按**相册属主**的可见集过滤" +
			"（List 用 r.ownerID、Get 用 d.OwnerID），兜住 AddItems 越权修复前遗留的历史脏行；③ addItemsQueries 的插入语句。" +
			"Get 有两个调用方：albums handler（HTTP 层先 canManage）与公开分享链路 shares.Store.ListItems（分享 token 授权）；" +
			"谓词主体刻意用相册属主而非调用者，因为匿名分享没有主体，改用调用者会把分享整条 fail-closed 打死。",
		Evidence: "albums/store.go List/Get/addItemsQueries；albums/handlers.go Get（canManage 先于 Store.Get）；shares/store.go ListItems。",
	},
	regKey("internal/folders/folders.go", "FROM media"): {
		UserFacing: true,
		Reason: "GET /folders/tree 的目录树与计数（面向用户）。非 owner/admin 由**内联**谓词 `AND owner_id = $1` 收窄到本人；" +
			"owner/admin 见全量是刻意的管理员语义。",
		Evidence: "folders/folders.go:69-83（:77-80 追加 owner_id = $1）。",
	},
	regKey("internal/geo/media_store.go", "FROM media"): {
		UserFacing: true,
		Reason: "四个地图端点（items / clusters / histogram / …）全部从 baseCond 派生，返回 media 文件名与精确经纬度；" +
			"原实现只有 gps/deleted_at/bbox，放大 bbox 即拿全站他人媒体（与 /media 系列同形事故）。" +
			"现谓词来自 mediascope.VisibleCondFor（alias=\"\" 裸列名）。注：扫描报的 :99 是注释，:196/:230/:269/:307 才是 SQL。",
		Evidence: "geo/media_store.go:90-116（:111 VisibleCondFor）；既有守卫 geo/media_store_visible_test.go。",
	},
	regKey("internal/search/store.go", "FROM media"): {
		UserFacing: true, // 取严：:85 hasGPS 只返回 bool，:156/:180 才是面向用户的结果/计数
		Reason: "① :85 hasGPS 只回答「库内是否存在带 GPS 的媒体」（bool，不返回行）；② :156/:180 是 GET /search 的计数与结果，" +
			"可见性谓词来自 mediascope.VisibleCond，与 media 侧共用同一段 sharedVerdict。",
		Evidence: "search/store.go:82-87、:155-182；search/query.go:195-196（mediascope.VisibleCond）；既有守卫 search/query_visible_test.go。",
	},
	regKey("internal/shares/store.go", "FROM media"): {
		UserFacing: true, // 取严：:203 是属主原语、:287/:299 只被已过 MediaInShare 的 handler 调用；:236 直接面向用户
		Reason: "① :236 ListItems(kind=media) 直接把这条媒体返回给持分享 token 的访问者 —— 授权来源是**分享 token 本身**" +
			"（sh.Kind==\"media\" 时目标即 sh.TargetID，不查他人集合）；② :203 TargetOwnedBy 是创建分享时的属主原语；" +
			"③ :287 ThumbFile / :299 HasHLS 只被 handler 在 MediaInShare 通过之后调用（公开缩略图/HLS 的越权防护）。",
		Evidence: "shares/store.go:196-215、:222-244、:280-307；shares/handlers.go:247 ListItems、:269→:278、:311→:320；:220 checkAccess(sh, 密码, now)。",
	},
	regKey("internal/spaces/spaces.go", "FROM media"): {
		UserFacing: true,
		Reason:     "GET /spaces 返回**调用者本人**个人空间的媒体数与字节数；WHERE 内联 `owner_id = $1 AND space = 'personal'` 绑定调用者。",
		Evidence:   "spaces/spaces.go:30-42（:33 userID 来自上下文，:38 内联 owner_id = $1）。",
	},
	regKey("internal/tags/store.go", "FROM media"): {
		UserFacing: true, // 取严：4 处是 worker/CLI，:137 才是被 handler 保护的读
		Reason: "① :63/:73 ListPendingAI、:108 ListEmbedded、:218 Counts 都是 taggen CLI / POST /ai/tags{scope:all} 用的全库扫描，" +
			"行只进服务端；② :137 LoadEmbedding 被 GET /ai/tags 使用，但 handler 在调用**之前**已 h.checkAccess（返回向量预览给有权者）。" +
			"取严记 true 正是为了把②那条 checkAccess 钉住。",
		Evidence: "tags/store.go:55-84、:99-131、:133-145（:137）、:205-220；media/tag_handlers.go:402-416（:408 checkAccess 先于 :416 LoadEmbedding）。",
	},
	regKey("internal/faces/people.go", "FROM media"): {
		UserFacing: true,
		Reason: "人物名单与人物封面：GET /ai/faces/people 返回媒体 id 与封面。可见性挂在 faces 的 LEFT JOIN 条件上" +
			"（mediascope.VisibleCondFor，m 与 mc 两个别名），不可见媒体的人脸行整体不参与聚合。",
		Evidence: "faces/people.go:80-110（:81-82 VisibleCondFor(1, userID, \"m\"/\"mc\")）；既有守卫 faces/people_scope_test.go。",
	},
	regKey("internal/faces/people.go", "JOIN media"): {
		UserFacing: true,
		Reason: "① :119 未命名聚类（同一 visMedia 谓词，INNER JOIN）；② :296 personMediaQuery（人物照片列表）——" +
			"此处曾是真实越权点：改造前 WHERE 只有 person_id + deleted_at，任一带 media:read 的账号知道人物 id 即可拿到他人媒体 ID 与文件名（已实测复现）。" +
			"现谓词 mediascope.VisibleCondFor(2, userID, \"m\")。",
		Evidence: "faces/people.go:114-126、:289-302（:290 VisibleCondFor）；既有守卫 faces/people_scope_test.go。",
	},
	regKey("internal/faces/people.go", "UPDATE media"): {
		UserFacing: false,
		Reason: "ResetScanned 复位 faces_scanned_at（POST /ai/faces {scope:all|<id>}），只返回影响行数，不交付 media 内容。" +
			"（scope=all 是跨用户**写**，不是内容泄漏 —— 见报告「观察」一节。）",
		Evidence: "faces/people.go:352-372；faces/api.go:132-148 TriggerScan。",
	},
	regKey("internal/transcode/transcode.go", "JOIN media"): {
		UserFacing: true,
		Reason: "JobStatus（GET /transcode/job/:id）把任务字段返回给用户。transcode_jobs 无 owner 列，故 JOIN media 取 owner_id，" +
			"再 canAccessMedia；无权与不存在共用 404 形状（避免存在性预言机）。注：扫描报的 :186 是注释，:199 才是 SQL。",
		Evidence: "transcode/transcode.go:184-210（:199 JOIN、:206 canAccessMedia）；既有守卫 transcode/access_test.go。",
	},
	regKey("internal/audit/store.go", "FROM media"): {
		UserFacing: false,
		Reason: "GET /admin/stats（契约 §14）只返回全站计数与存储字节数（count(*) / sum(filesize)），**不返回任何 media 行或内容**；" +
			"端点要求 admin:system 权限。",
		Evidence: "audit/store.go:259-270；cmd/api/main.go:385（RequirePerm(authStore, \"admin:system\")）。",
	},
}

// wiringRef 一条接线断言。
//
//	Func == "" → 在 File 全文中查找 Symbol；
//	Func != "" → 只在 File 里名为 Func 的函数**函数体内**查找 Symbol
//	             （这才是"这个端点真的判了归属"，而不是"文件里别处出现过这个词"）。
type wiringRef struct {
	File   string
	Func   string
	Symbol string
}

// visibilityWiring 面向终端用户的 media 查询文件 → 它接的可见性判定。
//
// 键必须与 registeredMediaQueries 里 UserFacing=true 的文件集合**完全一致**（双向检查，
// 见 TestMediaVisibility_UserFacingFilesAreWired）：少一项说明"登记了但没接"，
// 多一项说明"接线表里有悬挂条目"。
var visibilityWiring = map[string][]wiringRef{
	"internal/media/timeline.go": {
		{File: "internal/media/timeline.go", Symbol: "scopeConds("},
		{File: "internal/media/handlers.go", Func: "List", Symbol: "ResolveMediaScope("},
	},
	"internal/media/duplicates.go": {
		{File: "internal/media/duplicates.go", Symbol: "scopeConds("},
		{File: "internal/media/handlers.go", Func: "Duplicates", Symbol: "ResolveMediaScope("},
	},
	"internal/media/histogram.go": {
		{File: "internal/media/histogram.go", Symbol: "scopeConds("},
		{File: "internal/media/handlers.go", Func: "DateHistogram", Symbol: "ResolveMediaScope("},
	},
	"internal/media/tags.go": {
		{File: "internal/media/tags.go", Symbol: "scopeConds("},
		{File: "internal/media/tag_handlers.go", Func: "ListTagMedia", Symbol: "ResolveMediaScope("},
	},
	"internal/media/thumb.go": {
		{File: "internal/media/thumb.go", Func: "Thumb", Symbol: "h.thumbAccess("},
		{File: "internal/media/thumb.go", Func: "thumbAccess", Symbol: "h.Store.readAllowed"},
	},
	// Job000131：实时转码兜底端点——判定与 Download 完全同源（handler 层 checkReadAccess）。
	"internal/media/live_handlers.go": {
		{File: "internal/media/live_handlers.go", Func: "MasterM3U8", Symbol: "h.Media.checkReadAccess("},
		{File: "internal/media/live_handlers.go", Func: "Segment", Symbol: "h.Media.checkReadAccess("},
		{File: "internal/media/write_handlers.go", Func: "checkReadAccess", Symbol: "h.Store.readAccessOf"},
	},
	"internal/media/detail.go": {
		{File: "internal/media/write_handlers.go", Func: "Detail", Symbol: "h.checkReadAccess("},
		{File: "internal/media/write_handlers.go", Func: "checkReadAccess", Symbol: "h.Store.readAccessOf"},
	},
	"internal/media/upload.go": {
		{File: "internal/media/upload.go", Func: "Download", Symbol: "h.checkReadAccess("},
	},
	"internal/media/write.go": {
		{File: "internal/media/write.go", Symbol: "m.owner_id = $1"},
		{File: "internal/media/write_handlers.go", Func: "Trash", Symbol: `c.GetString("user_id")`},
	},
	"internal/albums/groups.go": {
		{File: "internal/albums/groups.go", Symbol: "mediascope.VisibleCondFor("},
		{File: "internal/albums/groups.go", Symbol: "a.owner_id = $1"},
	},
	"internal/albums/store.go": {
		{File: "internal/albums/store.go", Symbol: "a.owner_id = $1"},
		{File: "internal/albums/store.go", Symbol: "mediascope.VisibleCondFor("},
		{File: "internal/albums/handlers.go", Func: "Get", Symbol: "canManage("},
	},
	"internal/folders/folders.go": {
		{File: "internal/folders/folders.go", Func: "Tree", Symbol: "owner_id = $1"},
	},
	"internal/media/dav.go": {
		{File: "internal/media/dav.go", Symbol: "ReadCond("},
	},
	"internal/media/batch_store.go": {
		{File: "internal/media/batch_store.go", Symbol: "owner_id = $2"},
	},
	"internal/geo/media_store.go": {
		{File: "internal/geo/media_store.go", Symbol: "mediascope.VisibleCondFor("},
	},
	"internal/search/store.go": {
		{File: "internal/search/query.go", Symbol: "mediascope.VisibleCond("},
	},
	"internal/shares/store.go": {
		{File: "internal/shares/store.go", Symbol: "mediascope.VisibleCondFor("},
		{File: "internal/shares/handlers.go", Symbol: "h.Store.ListItems("},
		{File: "internal/shares/handlers.go", Symbol: "h.Store.MediaInShare("},
	},
	"internal/spaces/spaces.go": {
		{File: "internal/spaces/spaces.go", Func: "Get", Symbol: "owner_id = $1"},
	},
	"internal/tags/store.go": {
		{File: "internal/media/tag_handlers.go", Func: "AITagsPreview", Symbol: "h.checkAccess("},
	},
	"internal/faces/people.go": {
		{File: "internal/faces/people.go", Symbol: "mediascope.VisibleCondFor("},
		{File: "internal/faces/people.go", Func: "personMediaQuery", Symbol: "mediascope.VisibleCondFor("},
	},
	"internal/transcode/transcode.go": {
		{File: "internal/transcode/transcode.go", Func: "JobStatus", Symbol: "canAccessMedia("},
		{File: "internal/transcode/transcode.go", Func: "ServeHLS", Symbol: "canAccessMedia("},
	},
}

// TestMediaVisibility_UnregisteredMediaQueryFails 守卫 A。
func TestMediaVisibility_UnregisteredMediaQueryFails(t *testing.T) {
	root := backendRoot(t)
	groups, total, err := findUnregistered(root, registeredMediaQueries)
	if err != nil {
		t.Fatalf("遍历源码失败: %v", err)
	}

	// 前提自证：一条都没扫到时，"没有未登记项"就是一句空话
	// （特征正则写坏、扫描根目录算错、WalkDir 静默跳过……都会造成这种假绿）。
	if total == 0 {
		t.Fatalf("在 %s 的 internal/ + cmd/ 下**一条** media 查询都没扫到 —— "+
			"特征正则写坏了，或扫描根目录算错了。守卫的前提失效，先修它。", root)
	}

	if len(groups) > 0 {
		var sb strings.Builder
		fmt.Fprintf(&sb, "发现 %d 组**未登记**的 media 查询（本次共扫到 %d 处命中）——\n"+
			"新写的 media 查询必须登记，并回答一个问题：\n"+
			"「这条查询会不会把 media 行/内容返回给终端用户？」\n"+
			"  会   → 必须接上可见性/归属判定（mediascope.Conds/CondsFor/VisibleCond/VisibleCondFor，\n"+
			"         或具名 handler 层判定），登记为 UserFacing=true，并在 visibilityWiring 里写明判定落在哪；\n"+
			"  不会 → 登记为有意为之（UserFacing=false）并写清为什么不会泄漏。\n"+
			"⚠️ 不确定的请标「待确认」，不要猜（本项目已多次因「自造前提」翻车）。\n\n", len(groups), total)
		for _, g := range groups {
			fmt.Fprintf(&sb, "  %s  特征=%q\n     所在行: %v\n     源码: %s\n", g.File, g.Feature, g.Lines, g.Line1)
		}
		sb.WriteString("\n请在本文件 registeredMediaQueries 里补登记项。\n")
		t.Fatal(sb.String())
	}

	// 反向：登记表里不许有"仓库里已经不存在的命中"（否则守卫会慢慢与代码脱节而不自知）。
	live := map[string]bool{}
	hits, err := scanMediaQueries(root)
	if err != nil {
		t.Fatalf("遍历源码失败: %v", err)
	}
	for _, h := range hits {
		live[regKey(h.File, h.Feature)] = true
	}
	var stale []string
	for k := range registeredMediaQueries {
		if !live[k] {
			stale = append(stale, strings.ReplaceAll(k, "\x00", "  特征="))
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("登记表里有 %d 条**在仓库里已不存在**的条目 —— 查询被删/改写了特征，登记表却还留着，\n"+
			"守卫会因此慢慢与代码脱节。请删掉或更新这些条目：\n  %s", len(stale), strings.Join(stale, "\n  "))
	}
}

// TestMediaVisibility_UserFacingFilesAreWired 守卫 B。
func TestMediaVisibility_UserFacingFilesAreWired(t *testing.T) {
	root := backendRoot(t)

	// 1) 覆盖（正向）：登记为面向终端用户的文件，必须给出接线说明。
	userFacingFiles := map[string]bool{}
	for k, reg := range registeredMediaQueries {
		if !reg.UserFacing {
			continue
		}
		userFacingFiles[strings.SplitN(k, "\x00", 2)[0]] = true
	}
	var missing []string
	for f := range userFacingFiles {
		if _, ok := visibilityWiring[f]; !ok {
			missing = append(missing, f)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("下列文件登记为「面向终端用户」，但 visibilityWiring 里没有它的接线说明（= 登记了但忘了接）：\n  %s\n"+
			"请在 visibilityWiring 里写明判定落在哪个文件哪个函数。", strings.Join(missing, "\n  "))
	}

	// 2) 覆盖（反向）：不许有悬挂的接线条目。
	var dangling []string
	for f := range visibilityWiring {
		if !userFacingFiles[f] {
			dangling = append(dangling, f)
		}
	}
	sort.Strings(dangling)
	if len(dangling) > 0 {
		t.Fatalf("visibilityWiring 里有 %d 个**悬挂条目**：对应文件在登记表里没有 UserFacing=true 的条目。\n  %s",
			len(dangling), strings.Join(dangling, "\n  "))
	}

	// 3) 逐条断言接线真的在。
	files := make([]string, 0, len(visibilityWiring))
	for f := range visibilityWiring {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		for _, ref := range visibilityWiring[f] {
			ok, err := wiringRefSatisfied(root, ref)
			if err != nil {
				t.Fatalf("%s 的接线检查无法执行: %v", f, err)
			}
			if !ok {
				where := ref.File
				if ref.Func != "" {
					where = fmt.Sprintf("%s 的 %s 函数体内", ref.File, ref.Func)
				}
				t.Fatalf("%s 被登记为「面向终端用户」并声称可见性判定接在 %s 里的 %q，但**源码里找不到**。\n"+
					"这说明接线被摘掉了（或函数/符号被改名）—— 登记表与代码已经脱节。\n"+
					"若判定方式确实变了，请一并更新 visibilityWiring 与登记表的 Reason/Evidence。",
					f, where, ref.Symbol)
			}
		}
	}
}

// wiringRefSatisfied 检查一条接线断言是否成立。
func wiringRefSatisfied(root string, ref wiringRef) (bool, error) {
	path := filepath.Join(root, filepath.FromSlash(ref.File))
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	src := string(b)
	if ref.Func == "" {
		return strings.Contains(src, ref.Symbol), nil
	}
	body, ok := funcBody(src, ref.Func)
	if !ok {
		return false, fmt.Errorf("在 %s 里找不到名为 %q 的函数", ref.File, ref.Func)
	}
	return strings.Contains(body, ref.Symbol), nil
}

// funcBody 用 go/parser 取出文件里名为 name 的函数的函数体源码。
//
// 为什么不像内部测试那样按大括号朴素配平：Go 源码里的字符串（gin.H{...}、SQL）也含花括号，
// 朴素配平会被引号里的 { 带偏，产生"永远失败"的假守卫。用 AST 取 Body 的字节区间是精确的。
func funcBody(src, name string) (string, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, 0)
	if err != nil {
		return "", false
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != name || fn.Body == nil {
			continue
		}
		start := fset.Position(fn.Body.Pos()).Offset
		end := fset.Position(fn.Body.End()).Offset
		if start < 0 || end > len(src) || start >= end {
			return "", false
		}
		return src[start:end], true
	}
	return "", false
}

// TestMediaVisibility_GuardIsNotVacuous 守卫 C：守卫必须能真的失败。
//
// 本项目已记录两类教训 —— "永远通过的假测试"与"必然失败的断言"，两个方向都要避免。
// 本测试同时做三件事：
//  1. 把一个**未登记的新文件**放进一棵合成的最小仓库树，守卫必须命中它（证明"新文件会被抓住"）；
//  2. 从真实登记表里摘掉一条，那个**真实文件**必须立刻变成未登记（证明"登记表真的在起作用"）；
//  3. 反方向：_test.go 里的 SQL、以及 `FROM media_tags` 这类别的表，都不许被误判
//     （否则守卫会逼着后来者把登记表塞满，最后没人再维护它）。
func TestMediaVisibility_GuardIsNotVacuous(t *testing.T) {
	// ---- 1) 合成树：新文件必须被抓住 ----
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "internal", "newpkg", "query.go"),
		"package newpkg\n\nconst q = \"SELECT id FROM media WHERE deleted_at IS NULL\"\n")
	writeFile(t, filepath.Join(tmp, "cmd", "newcmd", "main.go"),
		"package main\n\nfunc main() { _ = \"UPDATE media SET rating = 1\" }\n")
	writeFile(t, filepath.Join(tmp, "internal", "newpkg", "query_test.go"),
		"package newpkg\n\nconst tq = \"SELECT id FROM media\"\n")
	writeFile(t, filepath.Join(tmp, "internal", "newpkg", "other.go"),
		"package newpkg\n\nconst oq = \"SELECT id FROM media_tags WHERE tag_id = 1\"\n")

	groups, total, err := findUnregistered(tmp, registeredMediaQueries)
	if err != nil {
		t.Fatalf("合成树扫描失败: %v", err)
	}
	if total != 2 {
		t.Fatalf("合成树里应扫到 2 处命中（internal 的新文件 + cmd 的新文件），实际 %d 处。"+
			"少了说明 _test.go 没被排除或别的表被误判；多了说明排除了不该排除的东西。", total)
	}
	got := map[string]bool{}
	for _, g := range groups {
		got[g.File+"|"+g.Feature] = true
	}
	for _, want := range []string{"internal/newpkg/query.go|FROM media", "cmd/newcmd/main.go|UPDATE media"} {
		if !got[want] {
			t.Fatalf("守卫失效：未登记的新文件没被抓住（缺 %s）。合成树实际命中：%v", want, got)
		}
	}

	// ---- 2) 摘掉一条真实登记：真实文件必须立刻变成未登记 ----
	pruned := make(map[string]mediaQueryRegistration, len(registeredMediaQueries))
	for k, v := range registeredMediaQueries {
		pruned[k] = v
	}
	const victim = "internal/media/thumb.go"
	delete(pruned, regKey(victim, "FROM media"))

	realRoot := backendRoot(t)
	groups2, _, err := findUnregistered(realRoot, pruned)
	if err != nil {
		t.Fatalf("真实仓库扫描失败: %v", err)
	}
	found := false
	for _, g := range groups2 {
		if g.File == victim && g.Feature == "FROM media" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("守卫失效：从登记表里摘掉 %s 的 FROM media 条目后，它**没有**变成未登记 —— "+
			"说明登记表根本没被用于判定，守卫是假的。", victim)
	}

	// ---- 3) 确认没动到真实仓库：撤销摘除后必须恢复零未登记 ----
	groups3, _, err := findUnregistered(realRoot, registeredMediaQueries)
	if err != nil {
		t.Fatalf("真实仓库复扫失败: %v", err)
	}
	if len(groups3) != 0 {
		t.Fatalf("撤销摘除后仍有 %d 组未登记命中：%v", len(groups3), groups3)
	}
}

// writeFile 在测试里写一个文件（自动建父目录）。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败 %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败 %s: %v", path, err)
	}
}
