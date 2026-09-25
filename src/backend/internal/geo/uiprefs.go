package geo

// 用户地图 UI 偏好（契约 §7 的 `GET/PUT /user/ui-prefs`）。
//
// 存 `user_ui_prefs`（PK = user_id，DDL 已带默认值），字段与契约一一对应：
//
//	map_slider_pos        时间轴滑块位置：bottom|top
//	map_filter_side       筛选栏所在侧：left|right
//	map_default_provider  默认底图源：auto|amap|osm
//	map_default_zoom      默认缩放级别；NULL = 不指定，交给前端自定
//
// # 两个刻意的取舍
//
//  1. **GET 永不 404**：DDL 给了默认值（bottom/left/auto/NULL），"从没设置过"与
//     "设置成了默认值"对使用者没有区别。让前端为"首次访问"单独写一个分支没有收益，
//     反而容易出现"首屏忘了取偏好"这类只有新用户才会遇到的缺陷。
//
//  2. **非法取值 400，不静默存下**：这四个值会被直接喂给地图初始化。
//     存进去一个拼错的 provider，故障现场是"地图打不开"，而根因在几百行之外 ——
//     宁可在写入时就报错。
//     但"字段缺失/空串"按**默认值**处理而不是报错：PUT 是整体替换语义，
//     让调用方每次都必须填满四个字段没有意义（少填一个就 400 只会让人困惑）。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	sliderBottom = "bottom"
	sliderTop    = "top"

	filterLeft  = "left"
	filterRight = "right"

	providerAuto = "auto"
	providerAmap = "amap"
	providerOSM  = "osm"

	markerIcon  = "icon"
	markerThumb = "thumb"

	minMapZoom = 1
	maxMapZoom = 20
)

// UIPrefs 用户地图 UI 偏好（契约字段名原样，前端可直接使用）。
type UIPrefs struct {
	MapSliderPos       string `json:"map_slider_pos"`
	MapFilterSide      string `json:"map_filter_side"`
	MapDefaultProvider string `json:"map_default_provider"`
	// MapDefaultZoom NULL 表示"不指定"。故用指针而非 int —— 0 是合法缩放级别吗？不是，
	// 但更重要的是要能区分"没设置"与"设置成 0"（后者应被拒）。
	MapDefaultZoom *int `json:"map_default_zoom"`
	// Job000059/060：筛选悬浮层收起状态 + 标记样式（icon|thumb，非法值 400 同其他枚举）。
	MapFilterCollapsed bool   `json:"map_filter_collapsed"`
	MapMarkerMode      string `json:"map_marker_mode"`
	// Job000101：空间页视图模式——false=时间轴平铺（默认）/true=按相册分组。
	SpacesGroupByAlbum bool `json:"spaces_group_by_album"`
	// Job000120：自动 HLS 转码开关——nil（PUT 缺省）= 保留现值（keep-on-absent，
	// 旧客户端不带此字段不致被清回默认）；显式 true/false 才写入。
	// GET 恒返回非 nil（默认 true = 保持存量行为）。
	AutoTranscode *bool `json:"auto_transcode"`
}

func boolPtr(b bool) *bool { return &b }

// DefaultUIPrefs 未设置时返回的默认值（与 DDL 默认值一致）。
func DefaultUIPrefs() UIPrefs {
	return UIPrefs{
		MapSliderPos:       sliderBottom,
		MapFilterSide:      filterLeft,
		MapDefaultProvider: providerAuto,
		MapMarkerMode:      markerIcon,
		AutoTranscode:      boolPtr(true),
	}
}

// NormalizeUIPrefs 归一化并校验 UI 偏好（纯函数，便于穷举单测）。
// 空串回落默认值；非空值必须命中白名单；zoom 若给出必须在 [1,20]。
func NormalizeUIPrefs(in UIPrefs) (UIPrefs, error) {
	out := DefaultUIPrefs()
	// zoom 先原样带过来（nil 保持 nil）
	out.MapDefaultZoom = in.MapDefaultZoom

	if v := strings.ToLower(strings.TrimSpace(in.MapSliderPos)); v != "" {
		if v != sliderBottom && v != sliderTop {
			return out, fmt.Errorf("map_slider_pos 仅支持 %s|%s", sliderBottom, sliderTop)
		}
		out.MapSliderPos = v
	}
	if v := strings.ToLower(strings.TrimSpace(in.MapFilterSide)); v != "" {
		if v != filterLeft && v != filterRight {
			return out, fmt.Errorf("map_filter_side 仅支持 %s|%s", filterLeft, filterRight)
		}
		out.MapFilterSide = v
	}
	if v := strings.ToLower(strings.TrimSpace(in.MapMarkerMode)); v != "" {
		if v != markerIcon && v != markerThumb {
			return out, fmt.Errorf("map_marker_mode 仅支持 %s|%s", markerIcon, markerThumb)
		}
		out.MapMarkerMode = v
	}
	out.MapFilterCollapsed = in.MapFilterCollapsed
	// 布尔无非法值空间（缺失= false = 默认平铺），原样透传即可。
	out.SpacesGroupByAlbum = in.SpacesGroupByAlbum
	// Job000120：auto_transcode 原样透传（nil 保持 nil = keep-on-absent 交给
	// PutUIPrefs 的 COALESCE 落库）。**不得**在此回落默认 true——Put 前 Normalize
	// 会把 nil 洗成 *true，keep-on-absent 即失效（部署验证第一轮 8/11 逮住的本缺陷）。
	// 「缺省=开启」只由两处负责：DefaultUIPrefs（GET 无行）与 DDL DEFAULT true（INSERT）。
	out.AutoTranscode = in.AutoTranscode

	if v := strings.ToLower(strings.TrimSpace(in.MapDefaultProvider)); v != "" {
		if v != providerAuto && v != providerAmap && v != providerOSM {
			return out, fmt.Errorf("map_default_provider 仅支持 %s|%s|%s", providerAuto, providerAmap, providerOSM)
		}
		out.MapDefaultProvider = v
	}
	if z := in.MapDefaultZoom; z != nil && (*z < minMapZoom || *z > maxMapZoom) {
		return out, fmt.Errorf("map_default_zoom 需在 %d..%d", minMapZoom, maxMapZoom)
	}
	return out, nil
}

// GetUIPrefs 读取当前用户 UI 偏好；无记录返回默认值（不返回错误，见文件头说明）。
func (s *MediaStore) GetUIPrefs(ctx context.Context, userID string) (UIPrefs, error) {
	out := DefaultUIPrefs()
	var slider, side, provider, marker string
	var zoom *int
	var collapsed, spacesGroup, autoTC bool
	err := s.Pool.QueryRow(ctx, `
		SELECT map_slider_pos, map_filter_side, map_default_provider, map_default_zoom,
		       map_filter_collapsed, map_marker_mode, spaces_group_by_album, auto_transcode
		FROM user_ui_prefs WHERE user_id = $1`, userID).Scan(&slider, &side, &provider, &zoom, &collapsed, &marker, &spacesGroup, &autoTC)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	// 行内值理论上都过过校验；仍走一次归一化，避免历史脏数据把非法值带到前端
	// （归一化对非法值会报错，此处降级为默认值而不是让 GET 失败）。
	got, nerr := NormalizeUIPrefs(UIPrefs{
		MapSliderPos: slider, MapFilterSide: side,
		MapDefaultProvider: provider, MapDefaultZoom: zoom,
		MapFilterCollapsed: collapsed, MapMarkerMode: marker,
		SpacesGroupByAlbum: spacesGroup,
		AutoTranscode:      boolPtr(autoTC),
	})
	if nerr != nil {
		return DefaultUIPrefs(), nil
	}
	return got, nil
}

// PutUIPrefs 覆盖写入（upsert）当前用户 UI 偏好。入参应已 Normalize。
// auto_transcode 例外地采用 keep-on-absent：NULL 时 INSERT 落默认 true、
// UPDATE 保留现值——旧客户端（不带此字段）整行 PUT 不会把用户已关的开关清回默认。
func (s *MediaStore) PutUIPrefs(ctx context.Context, userID string, p UIPrefs) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO user_ui_prefs (user_id, map_slider_pos, map_filter_side, map_default_provider, map_default_zoom,
		                           map_filter_collapsed, map_marker_mode, spaces_group_by_album, auto_transcode, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9, true), now())
		ON CONFLICT (user_id) DO UPDATE SET
			map_slider_pos       = EXCLUDED.map_slider_pos,
			map_filter_side      = EXCLUDED.map_filter_side,
			map_default_provider = EXCLUDED.map_default_provider,
			map_default_zoom     = EXCLUDED.map_default_zoom,
			map_filter_collapsed = EXCLUDED.map_filter_collapsed,
			map_marker_mode      = EXCLUDED.map_marker_mode,
			spaces_group_by_album = EXCLUDED.spaces_group_by_album,
			auto_transcode       = COALESCE($9, user_ui_prefs.auto_transcode),
			updated_at           = now()`,
		userID, p.MapSliderPos, p.MapFilterSide, p.MapDefaultProvider, p.MapDefaultZoom,
		p.MapFilterCollapsed, p.MapMarkerMode, p.SpacesGroupByAlbum, p.AutoTranscode)
	return err
}
