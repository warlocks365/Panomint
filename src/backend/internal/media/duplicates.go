package media

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"panoalbum/internal/phash"
)

// PRD §6.16 工具箱「重复项目」：用感知哈希（media.phash，见迁移 00024 与 internal/phash）
// 找出观感相同的副本，整组交给用户人工确认后删除多余的。
//
// 三个刻意的取舍：
//  1. 阈值默认 10（见 DuplicateThresholdDefault），是按真实数据实测标定的，不是拍脑袋；
//  2. 比较在 Go 里做两两全量，而不是把 bit_count 自连接写进 SQL ——
//     候选宇宙先限死 5000（MaxPhashUniverse），5000²/2 ≈ 1250 万次 popcount 在毫秒级，
//     换来的是分组逻辑（并查集/keeper）可以纯函数单测，且不依赖 phash 上的额外索引；
//  3. 分组取传递闭包（并查集），链式副本 A≈B、B≈C 合成一组。
//
// 本端点**只读**：不写 media.duplicate_of。那列无人写入、也没有既定的写入口约定，
// 擅自在这里落"谁是原件"的判定会把一个可回退的展示接口变成有副作用的数据迁移。

const (
	// DuplicateThresholdDefault 汉明距离阈值默认值（也是 UI 默认值）。
	//
	// 为什么恰好是 10 —— 测试库实测（真实照片）：
	//   · 容差侧：同一张图重新编码（PNG / JPEG q90 / q60 / q30）与缩放（75% / 60% / 40%）
	//     全部落在 d=0；真实照片的容差样本最大到 d=6。
	//   · 无关侧：990 组无关图对，中位数 32、p95 为 38，仅 3 组 ≤7（人工核对确为真近重复）。
	// 于是"容差 ≤6"与"无关 ≥14"之间空出 8 位，10 正好居中，两侧各留 4 位余量。
	// 改这个数等于改"什么算重复"的产品定义：必须重新标定，而不是当作调参。
	DuplicateThresholdDefault = 10
	DuplicateThresholdMin     = 0
	DuplicateThresholdMax     = 20

	// DuplicateLimitDefault 默认返回组数；DuplicateLimitMax 上限（与 List 的 limit 同量级）。
	DuplicateLimitDefault = 50
	DuplicateLimitMax     = 200

	// MaxPhashUniverse 参与两两比较的媒体数上限，超出直接 400 拒绝而不是挂起请求。
	//
	// 两两比较天生 O(N²)：N=5000 约 1250 万次异或 + popcount（毫秒级）；
	// N=50000 就是 12.5 亿次（十秒级且占满一个核）。与其让调用方等到超时，
	// 不如明确拒绝并把当前规模告诉他，让他按 space 缩小范围或分批处理。
	MaxPhashUniverse = 5000
)

// ErrInvalidDuplicateParams 参数非法（非数字）。合法但越界的数字走钳制，不报错。
var ErrInvalidDuplicateParams = errors.New("参数非法：threshold 需为整数，limit 需为整数")

// TooManyMediaError 候选宇宙超出 O(N²) 上限，携带真实规模供调用方决策。
type TooManyMediaError struct{ Size int }

func (e *TooManyMediaError) Error() string {
	return fmt.Sprintf("待比较媒体数 %d 超过上限 %d，请缩小范围（如按 space 分批）后重试", e.Size, MaxPhashUniverse)
}

// DuplicateParams GET /media/duplicates 查询参数（作用域字段语义与 ListParams 一致）。
type DuplicateParams struct {
	Scope     MediaScope // 空间作用域（唯一真源，见 scope.go；零值会被收敛为空结果）
	Threshold int
	Limit     int
}

// DuplicateGroup 一组互为重复的媒体。KeepID 是建议保留者（见 betterKeeper）。
type DuplicateGroup struct {
	KeepID string     `json:"keep_id"`
	Phash  string     `json:"phash"` // 保留者的指纹，十六进制（见 formatPhash）
	Items  []MediaRef `json:"items"`
}

// DuplicateResult 响应体。契约里此前没有这个端点，本结构即其定义。
type DuplicateResult struct {
	Groups    []DuplicateGroup `json:"groups"`
	Total     int              `json:"total"`     // 命中的组总数（limit 截断前）
	Scanned   int              `json:"scanned"`   // 实际参与比较的媒体数（已算指纹且未删、在作用域内）
	Threshold int              `json:"threshold"` // 实际生效阈值（钳制后）
	Truncated bool             `json:"truncated"` // groups 是否因 limit 被截断
}

// ParseDuplicateParams 解析并钳制查询参数。
//
// 分工是刻意的：**非数字 → 400**（这是调用方笔误，静默套默认值会让他以为筛选生效了）；
// **数字越界 → 钳制**（0/99 是对取值范围的偏差理解，钳制后仍符合其意图，且与 List 对
// limit 的处理一致：timeline.go:167 同样是钳制而非报错）。
func ParseDuplicateParams(thresholdRaw, limitRaw string) (threshold, limit int, err error) {
	threshold = DuplicateThresholdDefault
	if s := strings.TrimSpace(thresholdRaw); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil {
			return 0, 0, ErrInvalidDuplicateParams
		}
		threshold = clampInt(n, DuplicateThresholdMin, DuplicateThresholdMax)
	}
	limit = DuplicateLimitDefault
	if s := strings.TrimSpace(limitRaw); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil {
			return 0, 0, ErrInvalidDuplicateParams
		}
		limit = clampInt(n, 1, DuplicateLimitMax)
	}
	return threshold, limit, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// formatPhash 输出 "0x" + 16 位十六进制。
//
// 为什么不用十进制数字：指纹是 64 位**无符号**，bit 63 置位时值 ≥ 2^63，
// 而 JSON 数字在浏览器里会被解析成 double（JS Number），该量级已经丢精度 ——
// 0x8000000000000000 与 0x8000000000000001 在 double 里是同一个数。
// 十六进制字符串既不丢精度，又便于人工比对位模式。
func formatPhash(h uint64) string {
	return fmt.Sprintf("0x%016x", h)
}

// dupCandidate 参与比较的候选：分组只用 Phash，keeper 选择还要像素数 / 大小 / 时间。
//
// Items 直接复用 MediaRef（与 /media、/media/trash 同一形状），不另造序列化结构，
// 免得前端为工具箱再写一套解析。
type dupCandidate struct {
	Ref      MediaRef
	PHash    uint64
	Filesize int64
}

// betterKeeper 返回 a 是否比 b 更适合"保留"。
//
// 启发式「留最好那份」，判据按优先级：
//  1. 像素数 width*height —— 分辨率是观感损失最直接的度量，先保清晰度；
//  2. filesize 更大 —— 同分辨率下更大者通常码率/质量更高（重压缩过的副本更小）；
//  3. taken_at 更早 —— 同一张图的多次导入中，最早那份更接近"原始导入"；
//  4. id 更小 —— **必须有这一级兜底**：前三项都可能完全打平（同批导入的同名副本），
//     没有全序则同一份数据两次调用可能给出不同 keeper，"删除其余"在 UI 上就成了不确定操作。
func betterKeeper(a, b dupCandidate) bool {
	pa, pb := pixelCount(a.Ref), pixelCount(b.Ref)
	if pa != pb {
		return pa > pb
	}
	if a.Filesize != b.Filesize {
		return a.Filesize > b.Filesize
	}
	if !a.Ref.TakenAt.Equal(b.Ref.TakenAt) {
		return a.Ref.TakenAt.Before(b.Ref.TakenAt)
	}
	return a.Ref.ID < b.Ref.ID
}

func pixelCount(r MediaRef) int {
	if r.Width == nil || r.Height == nil {
		return 0
	}
	return *r.Width * *r.Height
}

// groupDuplicates 把候选按 Hamming ≤ threshold 分组，只输出 size ≥ 2 的组。
//
// 并查集求**传递闭包**：A≈B、B≈C 并成一组，即使 A 与 C 的直接距离已 > threshold。
// 代价是"桥接"——一个中间副本能把两端不太像的图拉进同一组；收益是同一张图的 5 份副本
// 只会得到一个组，用户清一次就干净了，而不是面对若干互相重叠的小簇。
// 这里接受桥接，因为**结果不自动删任何东西**：整组交给 UI 展示、由人判断。
// 若不做传递闭包，"删除其余"这个动作在链式副本上会退化成需要反复确认多次。
//
// 输出与输入顺序无关（组按 组大小降序 → keep_id 升序；组内按 betterKeeper 降序，
// 该比较链是全序），故同一份数据每次调用的结果一致。
func groupDuplicates(items []dupCandidate, threshold int) []DuplicateGroup {
	if len(items) < 2 {
		return []DuplicateGroup{}
	}
	parent := make([]int, len(items))
	for i := range parent {
		parent[i] = i
	}
	// find 迭代 + 路径折半（规模上限 5000，递归也行，但迭代不需要担心深度）
	find := func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if phash.Hamming(items[i].PHash, items[j].PHash) <= threshold {
				if ri, rj := find(i), find(j); ri != rj {
					parent[rj] = ri
				}
			}
		}
	}

	byRoot := map[int][]int{}
	for i := range items {
		r := find(i)
		byRoot[r] = append(byRoot[r], i)
	}

	groups := []DuplicateGroup{}
	for _, idxs := range byRoot {
		if len(idxs) < 2 { // 单元素组不是重复，只会给 UI 添噪音
			continue
		}
		sort.Slice(idxs, func(i, j int) bool { return betterKeeper(items[idxs[i]], items[idxs[j]]) })
		g := DuplicateGroup{
			KeepID: items[idxs[0]].Ref.ID, // 排序后首位即 keeper
			Phash:  formatPhash(items[idxs[0]].PHash),
			Items:  make([]MediaRef, 0, len(idxs)),
		}
		for _, k := range idxs {
			g.Items = append(g.Items, items[k].Ref)
		}
		groups = append(groups, g)
	}
	// map 迭代顺序随机，必须显式排序才能保证响应稳定（大组在前，更可能是用户想优先清理的）
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Items) != len(groups[j].Items) {
			return len(groups[i].Items) > len(groups[j].Items)
		}
		return groups[i].KeepID < groups[j].KeepID
	})
	return groups
}

// duplicateUniverseWhere 组装候选宇宙过滤条件。
//
// 作用域谓词**直接复用 scopeConds**（scope.go 的唯一真源），只在末尾追加
// `m.phash IS NOT NULL`（本端点特有的"必须已算出指纹"）。
//
// 这里曾经是"逐字抄一份 List 的谓词"，并在注释里写下"必须与 List 同步演进"——
// 事实证明了那句警告不够用：List 侧修改作用域时这里很容易被漏掉，而漏掉的后果是
// 一个用户的媒体出现在另一个用户的去重结果里（实测 viewer 账号确实拿到了
// owner 的 3 个重复组 / 6 条媒体 ID）。现在改为**共用同一个函数**，
// 从结构上消除漂移的可能，而不是靠注释提醒。
func duplicateUniverseWhere(p DuplicateParams) (string, []any) {
	conds := []string{"m.deleted_at IS NULL"}
	// 作用域谓词最先追加，占位符从 $1 起编号（见 scopeConds 的调用约定）
	scopeWhere, args := scopeConds(p.Scope)
	conds = append(conds, scopeWhere...)
	conds = append(conds, "m.phash IS NOT NULL")
	return strings.Join(conds, " AND "), args
}

// FindDuplicates 执行重复检测：先数候选规模，再取回候选并在内存里两两分组。
func (s *Store) FindDuplicates(ctx context.Context, p DuplicateParams) (*DuplicateResult, error) {
	if p.Limit <= 0 || p.Limit > DuplicateLimitMax {
		p.Limit = DuplicateLimitDefault
	}
	where, args := duplicateUniverseWhere(p)

	// 先数再查：只靠 LIMIT 探测只能报出"上限+1"这种失真数字，而调用方需要知道真实规模
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM media m WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	if total > MaxPhashUniverse {
		return nil, &TooManyMediaError{Size: total}
	}

	// ORDER BY id 只为让底层行序稳定（分组的输出顺序不依赖它，见 groupDuplicates）
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, m.type::text, COALESCE(m.filename,''), COALESCE(m.folder_path,''), m.taken_at,
		       m.width, m.height, m.duration, m.codec, m.is_360, m.place, m.rating,
		       m.thumbnail_sm, m.thumbnail_md, m.thumbnail_lg, m.phash, COALESCE(m.filesize, 0)
		FROM media m WHERE `+where+`
		ORDER BY m.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cands := make([]dupCandidate, 0, total)
	for rows.Next() {
		var (
			c     dupCandidate
			taken *time.Time
			ph    int64
		)
		if err := rows.Scan(&c.Ref.ID, &c.Ref.Type, &c.Ref.Filename, &c.Ref.FolderPath, &taken,
			&c.Ref.Width, &c.Ref.Height, &c.Ref.Duration, &c.Ref.Codec, &c.Ref.Is360,
			&c.Ref.Place, &c.Ref.Rating, &c.Ref.ThumbnailSM, &c.Ref.ThumbnailMD, &c.Ref.ThumbnailLG,
			&ph, &c.Filesize); err != nil {
			return nil, err
		}
		// taken_at 列可空，而 MediaRef.TakenAt 是非指针（与 /media 同一形状，不能改）。
		// 用 *time.Time 承接再回填，NULL 时保持零值——比让整个请求 500 合理。
		if taken != nil {
			c.Ref.TakenAt = *taken
		}
		// BIGINT 装的是无符号 64 位指纹的位模式，原样转回即可（见 internal/phash/store.go 的说明）
		c.PHash = uint64(ph)
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	groups := groupDuplicates(cands, p.Threshold)
	res := &DuplicateResult{
		Groups:    groups,
		Total:     len(groups),
		Scanned:   total, // == len(cands)（查询无 LIMIT），用 count 的结果更直接表达"参与比较的规模"
		Threshold: p.Threshold,
	}
	if len(res.Groups) > p.Limit {
		res.Groups = res.Groups[:p.Limit]
		res.Truncated = true
	}
	return res, nil
}
