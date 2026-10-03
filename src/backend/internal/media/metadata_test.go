package media

// Job000143 元数据写入的纯逻辑测试（不碰数据库）。
//
// 覆盖三态语义、坐标成对、范围校验 —— 这几条是「用户清空字段能不能真的清空」
// 与「半截坐标会不会污染库」的唯一防线，必须穷举。

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeMetadata_ThreeState(t *testing.T) {
	t.Run("缺席=不动", func(t *testing.T) {
		m, err := NormalizeMetadata(MetadataUpdate{})
		if err != nil {
			t.Fatalf("零值应通过校验: %v", err)
		}
		if !m.Empty() {
			t.Fatal("零值应判定为 Empty（handler 靠它拒绝无意义 UPDATE）")
		}
	})

	t.Run("只给 place=空串 → 清空而非不动", func(t *testing.T) {
		m, err := NormalizeMetadata(MetadataUpdate{SetPlace: true, Place: ""})
		if err != nil {
			t.Fatalf("清空是合法操作: %v", err)
		}
		if m.Empty() {
			t.Fatal("SetPlace=true 即便值为空也不算 Empty（要写 NULL）")
		}
		if !m.SetPlace || m.Place != "" {
			t.Fatalf("清空语义丢失: %+v", m)
		}
	})

	t.Run("只给 taken_at=零值 → 清空", func(t *testing.T) {
		m, _ := NormalizeMetadata(MetadataUpdate{SetTakenAt: true})
		if !m.SetTakenAt || !m.TakenAt.IsZero() {
			t.Fatalf("时间清空语义丢失: %+v", m)
		}
	})

	t.Run("部分字段：动的设 true、不动的保持 false", func(t *testing.T) {
		m, _ := NormalizeMetadata(MetadataUpdate{SetAddress: true, Address: "北京市东城区景山前街 4 号"})
		if !m.SetAddress {
			t.Fatal("address 应为 Set")
		}
		if m.SetPlace || m.SetTakenAt || m.SetGPS {
			t.Fatal("未提供的字段不得被标记为 Set（三态串味）")
		}
	})
}

func TestNormalizeMetadata_CoordsMustPair(t *testing.T) {
	cases := []struct {
		name string
		m    MetadataUpdate
	}{
		{"只给 lat", MetadataUpdate{SetGPS: true, Lat: 39.9}},
		{"只给 lng", MetadataUpdate{SetGPS: true, Lng: 116.4}},
		{"lat 有值 lng 为 0", MetadataUpdate{SetGPS: true, Lat: 39.9, Lng: 0}},
		{"lng 有值 lat 为 0", MetadataUpdate{SetGPS: true, Lat: 0, Lng: 116.4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeMetadata(tc.m)
			if !errors.Is(err, ErrHalfCoords) {
				t.Fatalf("半截坐标必须被拒（ErrHalfCoords），实际 err=%v", err)
			}
		})
	}
}

func TestNormalizeMetadata_CoordRange(t *testing.T) {
	cases := []struct {
		name    string
		lat, lng float64
		wantErr bool
	}{
		{"北京天安门", 39.9087, 116.3975, false},
		{"边界 90/180", 90, 180, false},
		{"边界 -90/-180", -90, -180, false},
		{"南极纬度越界", 91, 0, true},
		{"纬度负向越界", -91, 0, true},
		{"经度正向越界", 0, 181, true},
		{"经度负向越界", 0, -181, true},
		{"纽约（境外，范围仍合法）", 40.7128, -74.006, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeMetadata(MetadataUpdate{SetGPS: true, Lat: tc.lat, Lng: tc.lng})
			if tc.wantErr && !errors.Is(err, ErrBadMetadata) {
				t.Fatalf("越界必须被拒（ErrBadMetadata），实际 err=%v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("合法坐标被误拒: %v", err)
			}
		})
	}
}

func TestNormalizeMetadata_ZeroIsClearNotError(t *testing.T) {
	// (0,0) 是「清空 gps」的约定值，必须通过校验。
	if _, err := NormalizeMetadata(MetadataUpdate{SetGPS: true}); err != nil {
		t.Fatalf("(0,0) 应视为清空而非越界，实际: %v", err)
	}
	if _, err := NormalizeMetadata(MetadataUpdate{SetGPS: true, Lat: 0, Lng: 0}); err != nil {
		t.Fatalf("显式 (0,0) 应视为清空，实际: %v", err)
	}
}

func TestNormalizeMetadata_LengthLimits(t *testing.T) {
	t.Run("place 超 256 拒", func(t *testing.T) {
		_, err := NormalizeMetadata(MetadataUpdate{SetPlace: true, Place: strings.Repeat("景", 257)})
		if !errors.Is(err, ErrBadMetadata) {
			t.Fatalf("place 超长必须被拒，实际: %v", err)
		}
	})
	t.Run("place 恰好 256 通过", func(t *testing.T) {
		if _, err := NormalizeMetadata(MetadataUpdate{SetPlace: true, Place: strings.Repeat("景", 256)}); err != nil {
			t.Fatalf("边界长度不该被拒: %v", err)
		}
	})
	t.Run("address 超 512 拒", func(t *testing.T) {
		_, err := NormalizeMetadata(MetadataUpdate{SetAddress: true, Address: strings.Repeat("北", 513)})
		if !errors.Is(err, ErrBadMetadata) {
			t.Fatalf("address 超长必须被拒，实际: %v", err)
		}
	})
}

func TestNormalizeMetadata_TrimsWhitespace(t *testing.T) {
	m, _ := NormalizeMetadata(MetadataUpdate{
		SetPlace:   true,
		Place:      "  景山前街  ",
		SetAddress: true,
		Address:    "\t北京市东城区景山前街 4 号\n",
	})
	if m.Place != "景山前街" {
		t.Errorf("place 未去首尾空白: %q", m.Place)
	}
	if m.Address != "北京市东城区景山前街 4 号" {
		t.Errorf("address 未去首尾空白: %q", m.Address)
	}
}

// TestSetMetadataEmptyIsNoop Empty 时不该发 SQL。
// 这里只能验证判定（不触库）；SQL 层靠 handler 的 if !meta.Empty() 守卫。
func TestSetMetadataEmptyIsNoop(t *testing.T) {
	// 构造一个零值 Store 不应 panic —— 证明 Empty 判定发生在触库之前。
	var s *Store
	if err := s.SetMetadata(nil, "x", MetadataUpdate{}); err != nil {
		t.Fatalf("Empty 判定应先于触库短路，不该返回 err: %v", err)
	}
}

// TestTakenAtZeroValueDetection TakenAt 的零值是 time.Time{}，
// 必须与「1970-01-01」区分开（后者是合法时间）。
func TestTakenAtZeroValueDetection(t *testing.T) {
	var zero time.Time
	if !zero.IsZero() {
		t.Fatal("零值时间应 IsZero()")
	}
	epoch := time.Unix(0, 0).UTC()
	if epoch.IsZero() {
		t.Fatal("Unix 纪元不是 Go 的零值时间（IsZero 只认 year 1）")
	}
}
