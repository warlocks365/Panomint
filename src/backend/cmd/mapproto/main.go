// mapproto 地图模式原型服务（T2.2）。
// 端点：/api/map/items（bbox+zoom 聚合）、/tiles/amap|osm 瓦片反代、静态页托管。
// 运行：go run ./cmd/mapproto [-seed]  （-seed 写入合成 GPS 测试数据后退出）
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/geo"
)

func pgDSN() string {
	if v := os.Getenv("PG_DSN"); v != "" {
		return v
	}
	return "postgres://pano:PanoDev2026!@192.168.1.115:5432/pano_album"
}

func main() {
	seed := flag.Bool("seed", false, "写入合成测试数据后退出")
	flag.Parse()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgDSN())
	if err != nil {
		log.Fatalf("连接 PG 失败: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("PG 不可达: %v", err)
	}

	if *seed {
		if err := seedData(ctx, pool); err != nil {
			log.Fatalf("seed 失败: %v", err)
		}
		return
	}

	store := &geo.Store{Pool: pool, Table: "map_proto_points"}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/map/items", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := func(k string, def float64) float64 {
			v, _ := strconv.ParseFloat(q.Get(k), 64)
			if v == 0 {
				return def
			}
			return v
		}
		zoom, _ := strconv.Atoi(q.Get("zoom"))
		provider := q.Get("provider")
		if provider == "" {
			provider = "osm"
		}
		clusters, err := store.Clusters(ctx,
			f("min_lng", -180), f("min_lat", -85), f("max_lng", 180), f("max_lat", 85),
			zoom, provider)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"clusters": clusters, "zoom": zoom, "provider": provider})
	})

	// 瓦片反代（AC-16：前端不暴露 key；用户已确认 Q4 仅直转）
	mux.HandleFunc("GET /tiles/amap/{z}/{x}/{y}", tileProxy(mapAmap))
	mux.HandleFunc("GET /tiles/osm/{z}/{x}/{y}", tileProxy(mapOSM))

	// 静态页
	mux.Handle("/", http.FileServer(http.Dir("../../prototypes/map-mode")))

	addr := ":8081"
	log.Printf("地图原型服务 http://localhost%s （静态页 /，API /api/map/items）", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func mapAmap(z, x, y string) string {
	return fmt.Sprintf("https://webrd01.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x=%s&y=%s&z=%s", x, y, z)
}
func mapOSM(z, x, y string) string {
	return fmt.Sprintf("https://tile.openstreetmap.org/%s/%s/%s.png", z, x, y)
}

// tileProxy 瓦片反代：带 UA（OSM 要求）、透传内容类型与缓存头。
func tileProxy(mapper func(z, x, y string) string) http.HandlerFunc {
	client := &http.Client{Timeout: 15 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		url := mapper(r.PathValue("z"), r.PathValue("x"), r.PathValue("y"))
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "PanoAlbumProto/0.1 (map-mode prototype)")
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "上游瓦片获取失败", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(resp.StatusCode)
		buf := make([]byte, 32*1024)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}
}

// seedData 合成测试数据（用户已确认 Q3）：全球随机 + 中国城市群聚类 + 国外城市。
func seedData(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS map_proto_points (
			id   BIGSERIAL PRIMARY KEY,
			geom geometry(Point, 4326) NOT NULL,
			city VARCHAR(64)
		)`); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_mpp_geom ON map_proto_points USING GIST (geom)`); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `TRUNCATE map_proto_points`); err != nil {
		return err
	}

	rng := rand.New(rand.NewSource(42))
	type city struct {
		name string
		lng, lat, spread float64
		count int
	}
	clusters := []city{
		{"北京", 116.40, 39.90, 0.35, 800},
		{"上海", 121.47, 31.23, 0.30, 600},
		{"广州", 113.26, 23.13, 0.25, 400},
		{"深圳", 114.06, 22.54, 0.20, 300},
		{"成都", 104.07, 30.57, 0.25, 250},
		{"杭州", 120.15, 30.27, 0.20, 150},
		{"东京", 139.69, 35.68, 0.30, 200},
		{"纽约", -74.00, 40.71, 0.35, 180},
		{"巴黎", 2.35, 48.85, 0.25, 120},
		{"悉尼", 151.21, -33.87, 0.30, 80},
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	total := 0
	insert := func(lng, lat float64, name string) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO map_proto_points (geom, city) VALUES (ST_SetSRID(ST_MakePoint($1,$2),4326), $3)`,
			lng, lat, name)
		return err
	}
	for _, c := range clusters {
		for i := 0; i < c.count; i++ {
			if err := insert(c.lng+rng.NormFloat64()*c.spread, c.lat+rng.NormFloat64()*c.spread, c.name); err != nil {
				return err
			}
			total++
		}
	}
	for i := 0; i < 2000; i++ { // 全球随机
		if err := insert(rng.Float64()*360-180, rng.Float64()*140-70, "random"); err != nil {
			return err
		}
		total++
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Printf("seed 完成：%d 个测试点（中国 6 城市聚类 + 国外 4 城市 + 全球随机 2000）\n", total)
	return nil
}
