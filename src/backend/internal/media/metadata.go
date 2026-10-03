package media

// Job000143：媒体地理与时间元数据的写入（**只改 media 表，绝不触碰原媒体文件**）。
//
// 硬约束：本文件所有操作都是 `UPDATE media SET ...` —— 不写 EXIF、不改 sidecar、
// 不 open path 指向的 NAS 文件。验收标准 AC-09 以「原文件 mtime 与大小不变」为准。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 元数据写入的哨兵错误（handler 层映射为 400）。
var (
	// ErrBadMetadata 元数据取值非法（时间格式 / 坐标越界）。
	ErrBadMetadata = errors.New("元数据取值非法")
	// ErrHalfCoords 只提供了 lat 或 lng 其中之一。
	ErrHalfCoords = errors.New("经纬度必须成对提供")
)

// MetadataUpdate 一次元数据更新的输入。
//
// **三态语义**（每字段独立）：
//   - Set* 为 false → 该字段**不动**；
//   - Set* 为 true 且值为零值 → 该字段**清空**（置 NULL）。
//
// 为什么不用指针：指针的 nil/非nil 只能表达两态，无法区分「不改」与「清空」。
// 这里用显式 Set 标志位承载第三态，语义在归一化后由 SQL 的 CASE WHEN 消费。
//
// ⚠️ 坐标一律是 **WGS-84**（与 media.gps 的 SRID 4326 一致）。
// 上游（高德 GCJ-02）坐标必须在 handler 层转好再进来 —— 见 ConvertAmapToWGS84。
type MetadataUpdate struct {
	// TakenAt 拍摄时间；Set=true 且 Time 为零值 → 清空。
	TakenAt    time.Time
	SetTakenAt bool

	// Place 拍摄地短地名；Set=true 且为 "" → 清空。
	Place    string
	SetPlace bool

	// Address 详细地址；Set=true 且为 "" → 清空。
	Address    string
	SetAddress bool

	// Lat/Lng WGS-84 经纬度；Set=true 时两者一并生效（成对，见 ErrHalfCoords）。
	// SetGPS=true 且 Lat/Lng 均为 0 → 清空 gps。
	Lat    float64
	Lng    float64
	SetGPS bool
}

// NormalizeMetadata 校验并归一化元数据输入（handler 层调用）。
//
// 校验项：
//  1. 坐标成对：只给一个直接拒（半截坐标会污染 geometry 列，且用户无法察觉）；
//  2. 坐标范围：lat ∈ [-90,90]、lng ∈ [-180,180]（含边界值）；
//  3. 长度上限：place ≤ 256（对齐迁移 00047 放宽后的列宽）、
//     address ≤ 512（TEXT 但仍限长，防超长输入撑爆详情响应）。
func NormalizeMetadata(m MetadataUpdate) (MetadataUpdate, error) {
	if m.SetGPS {
		hasLat, hasLng := m.Lat != 0, m.Lng != 0
		// ⚠️ 顺序要紧：先判**范围**，再判成对。
		// 反过来的话 lat=91/lng=0 这类「单边越界」会先撞上成对校验（hasLat≠hasLng），
		// 报出「必须成对」而把「纬度越界」这个真正的问题吞掉 —— 调用方无从得知该改什么。
		// 单元测试 TestNormalizeMetadata_CoordRange 的越界用例锁死了这个顺序。
		if hasLat && (m.Lat < -90 || m.Lat > 90) {
			return m, fmt.Errorf("%w: 纬度须在 [-90,90]，当前 %g", ErrBadMetadata, m.Lat)
		}
		if hasLng && (m.Lng < -180 || m.Lng > 180) {
			return m, fmt.Errorf("%w: 经度须在 [-180,180]，当前 %g", ErrBadMetadata, m.Lng)
		}
		if hasLat != hasLng {
			return m, ErrHalfCoords
		}
		// (0,0) 是「清空 gps」的约定值。到这里范围已全部合法，无需再判。
		// 真实坐标不可能恰好是 (0,0)（几内亚湾海域），即使用户真填 (0,0)，
		// 语义上也等同于「无位置」，与清空不可区分 —— 这是有意的取舍。
	}
	if m.SetPlace && len([]rune(m.Place)) > 256 {
		return m, fmt.Errorf("%w: 拍摄地最长 256 字", ErrBadMetadata)
	}
	if m.SetAddress && len([]rune(m.Address)) > 512 {
		return m, fmt.Errorf("%w: 详细地址最长 512 字", ErrBadMetadata)
	}
	// 字符串统一去首尾空白（用户从搜索结果粘贴常带空白）。
	if m.SetPlace {
		m.Place = strings.TrimSpace(m.Place)
	}
	if m.SetAddress {
		m.Address = strings.TrimSpace(m.Address)
	}
	return m, nil
}

// Empty 判断归一化后是否**没有任何**字段需要写入（全 false 或全清空）。
// handler 用它给出 400 而不是发一条无意义的 UPDATE。
func (m MetadataUpdate) Empty() bool {
	return !m.SetTakenAt && !m.SetPlace && !m.SetAddress && !m.SetGPS
}

// SetMetadata 写元数据（单条 UPDATE + CASE WHEN，一次往返）。
//
// **为什么用 CASE WHEN $flag 而不是 COALESCE($new, old)**：
// COALESCE 的语义是「参数为 NULL 就用旧值」，无法表达「显式清空」——
// 用户清空拍摄地时传 NULL 会被 COALESCE 还原成旧值，功能上不可用。
// 用显式 flag 参数才能区分「不动」（flag=false）与「清空」（flag=true + 值零）。
//
// 坐标写入用 ST_SetSRID(ST_MakePoint(lng, lat), 4326)：注意经度在前（X=lng）。
func (s *Store) SetMetadata(ctx context.Context, id string, m MetadataUpdate) error {
	if m.Empty() {
		return nil
	}
	// 清空语义：flag=true 时把值转成可写入的零值（时间零值/空串/坐标 nil）。
	var takenAt any
	if m.SetTakenAt && !m.TakenAt.IsZero() {
		takenAt = m.TakenAt
	}
	// 空串在 SQL 侧用 nullif(…, '') 转成 NULL —— 列语义是「无值」而非「空字符串」。
	var place, address any
	if m.SetPlace {
		place = m.Place
	}
	if m.SetAddress {
		address = m.Address
	}
	var gpsWKT any
	if m.SetGPS && (m.Lat != 0 || m.Lng != 0) {
		// WKT 的几何顺序是 (x y) = (lng lat)
		gpsWKT = fmt.Sprintf("POINT(%g %g)", m.Lng, m.Lat)
	}

	ct, err := s.Pool.Exec(ctx, `
		UPDATE media SET
			taken_at = CASE WHEN $2::boolean THEN $3::timestamptz ELSE taken_at END,
			place    = CASE WHEN $4::boolean THEN nullif($5::varchar, '') ELSE place END,
			address  = CASE WHEN $6::boolean THEN nullif($7::text, '')     ELSE address END,
			gps      = CASE WHEN $8::boolean
			                  THEN CASE WHEN $9::text IS NULL THEN NULL
			                         ELSE ST_SetSRID(ST_GeomFromText($9::text), 4326) END
			                  ELSE gps END,
			updated_at = now()
		WHERE id = $1::uuid AND deleted_at IS NULL`,
		id,
		m.SetTakenAt, takenAt,
		m.SetPlace, place,
		m.SetAddress, address,
		m.SetGPS, gpsWKT,
	)
	if err != nil {
		return err
	}
	// 0 行 = 媒体不存在**或已软删**（回收站）。
	//
	// ⚠️ 必须检查，否则对回收站媒体发 PATCH 会返回 200 且回显新值，
	// 但实际一行都没写 —— 前端据此显示「已保存」，刷新即回退，
	// 用户完全无法察觉丢失。同包的 SetNotes/SetEdits 都有此检查，
	// 这里漏了会让两条写路径口径不一致。
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
