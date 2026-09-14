package faces

// 重扫时的「人脸身份迁移」：把旧检出上的用户命名关联搬到新检出上。
//
// 为什么需要它：为了让重扫幂等，scanOne 在写入前会**无条件**删除该媒体的全部 faces 行
// （见 cmd/facesgen/main.go）。而 faces.person_id 承载的是**用户手工命名**的结果
// （PeopleView → POST /people），删除会连带丢掉它。故删除前先把旧行读出来，
// 用几何重叠把命名迁移到新检出上——重扫只刷新几何与特征，不牺牲用户的命名劳动。
//
// 匹配规则刻意保持简单（不做匈牙利匹配、不做二次分配）：取与新人脸框 IoU 最大的旧脸，
// 且 IoU 必须达到 MatchMinIoU。依据：同一张媒体内不同人脸的位置互不重叠（IoU 通常≈0），
// 不会串味；而同一张脸在同一 LG 缩略图上重新检测时框的漂移远小于半个框，IoU 稳定 > 0.9。

import "math"

// MatchMinIoU 判定「新旧检出是同一张脸」的最小框重叠度（IoU）。
//
// 取 0.5 是目标检测领域判定「同一目标」的惯用阈值：同图同模型重扫时实测 IoU 通常 > 0.9，
// 0.5 留出了足够余量去吸收「换模型 / 调 faceconf·facenms·minpx」带来的框漂移。
// 低于该值的旧脸视为「这张脸已不再检出」，其命名关联随之丢弃——该人物若在别的媒体
// 也有脸，人物本身与那些脸不受影响（只有「仅出现在本媒体」的命名会彻底消失）。
const MatchMinIoU = 0.5

// FaceRef 某媒体已入库的一张人脸，用于重扫时迁移命名关联。
//
// 坐标语义与 Detection 一致（X/Y 为左上角，W/H 为宽高，单位=LG 缩略图像素），
// 故可直接与 Detection 做 IoU 比较。
type FaceRef struct {
	PersonID   string // 空串 = 未命名（该脸的 cluster_id 仍属"未命名聚类"）
	IsPet      bool
	ClusterID  string // 旧簇 ID；已命名的人脸重扫时沿用它，见 cmd/facesgen/main.go
	X, Y, W, H float64
}

// BoxIoU 两个人脸框的交并比。任一框宽或高非正（含坐标全 0 的占位框）时返回 0。
func BoxIoU(a, b FaceRef) float64 {
	ix := math.Min(a.X+a.W, b.X+b.W) - math.Max(a.X, b.X)
	iy := math.Min(a.Y+a.H, b.Y+b.H) - math.Max(a.Y, b.Y)
	if ix <= 0 || iy <= 0 {
		return 0
	}
	inter := ix * iy
	union := a.W*a.H + b.W*b.H - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

// BestFaceMatch 在 olds 中找出与 d 重叠度最高、且 ≥ MatchMinIoU 的旧人脸。
//
// 命中返回该旧脸（其 PersonID 可能为空串，即旧脸也未命名）与 true；
// 无合格者返回零值与 false（调用方据此视为「新检出的人脸」，不迁移任何关联）。
func BestFaceMatch(olds []FaceRef, d Detection) (FaceRef, bool) {
	now := FaceRef{X: d.X, Y: d.Y, W: d.W, H: d.H}
	var (
		best    FaceRef
		bestIoU = MatchMinIoU // 阈值本身即「恰好合格」的下界，首个合格者直接命中
		found   bool
	)
	for _, o := range olds {
		iou := BoxIoU(now, o)
		if iou < MatchMinIoU {
			continue
		}
		if !found || iou > bestIoU {
			best, bestIoU, found = o, iou, true
		}
	}
	return best, found
}
