package faces

// 增量聚类：新人脸与**已有簇质心**比较，相似度达到合并阈值则归入该簇，否则新建簇。
//
// 为什么与质心而非任一成员比较：避免「链式误合并」——若与最近邻单点比较，
// A~B、B~C 会经由 B 把明明不像的 A、C 并成一簇。
//
// 质心由 DB 侧按 `avg(embedding)` 现算（不额外建表/不落盘）：
// 批内逐张写入 faces 行后，后一张脸自然能查到前一张刚建成的簇（单成员时质心=其自身）。

import (
	"crypto/rand"
	"fmt"
	"math"
	"time"
)

// NewClusterID 生成新的聚类 ID（UUID v4 文本，faces.cluster_id 为 VARCHAR(64)）。
func NewClusterID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 熵源异常时退化为时间戳（极不可能触发），保证聚类流程不中断
		return fmt.Sprintf("c%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ClusterRef 一个已有簇的代表向量（质心；余弦比较与模长无关）。
type ClusterRef struct {
	ClusterID string
	Centroid  []float32
}

// PickCluster 返回与 emb 最相似且相似度 ≥ mergeSim 的簇 ID；无满足者返回空串（调用方新建簇）。
//
// 第二个返回值是命中的相似度（未命中时为全局最高相似度，便于日志与阈值标定）。
func PickCluster(emb []float32, refs []ClusterRef, mergeSim float64) (string, float64) {
	bestID := ""
	bestSim := math.Inf(-1)
	for _, r := range refs {
		if r.ClusterID == "" || len(r.Centroid) != len(emb) {
			continue
		}
		if sim := CosineSimilarity(emb, r.Centroid); sim > bestSim {
			bestSim, bestID = sim, r.ClusterID
		}
	}
	if bestSim >= mergeSim {
		return bestID, bestSim
	}
	return "", bestSim
}
