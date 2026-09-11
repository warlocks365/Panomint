package geo

// 用户地图偏好存储（Job000009）：聚合点图标配置账户级持久化。
//
// 数据存 user_preferences.map_icon（JSONB），跨设备同步；写失败优雅降级
// （前端仍可用 localStorage 兜底）。

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// MapIconPref 地图聚合点图标配置。
// Shape: circle|triangle|diamond|star（矢量形状）或 pin|inverted（内置 PNG）或 custom（上传）。
// Color: 色卡十六进制（仅矢量形状生效）。
// DataURL: 自定义 PNG 的 data URL（仅 shape=custom 生效，前端限制 ≤ 512KB）。
type MapIconPref struct {
	Shape  string `json:"shape"`
	Color  string `json:"color"`
	DataURL string `json:"data_url,omitempty"`
}

// ErrPrefNotFound 无偏好记录。
var ErrPrefNotFound = errors.New("偏好未设置")

// GetMapIcon 读取用户地图图标偏好；无记录返回 ErrPrefNotFound。
func (s *MediaStore) GetMapIcon(ctx context.Context, userID string) (*MapIconPref, error) {
	var raw []byte
	err := s.Pool.QueryRow(ctx,
		`SELECT map_icon FROM user_preferences WHERE user_id = $1`, userID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPrefNotFound
	}
	if err != nil {
		return nil, err
	}
	var p MapIconPref
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// PutMapIcon 写入（upsert）用户地图图标偏好。
func (s *MediaStore) PutMapIcon(ctx context.Context, userID string, p *MapIconPref) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO user_preferences (user_id, map_icon, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (user_id) DO UPDATE SET map_icon = EXCLUDED.map_icon, updated_at = now()`,
		userID, raw)
	return err
}
