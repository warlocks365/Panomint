package faces

// 多尺度检测（Job000041）：大小脸兼得。
//
// 背景（2026-09-20 测试服 benchmark，官方 yunet_2026may 动态模型，conf 0.87 / minpx 24，
// 14 张人群语料，详见 face.go 的实测表与登记簿 Job000041）：
//
//	单一 640（旧静态模型）：62 张；单一 1280（自适应）：82 张（+32%），但 plueschow
//	侧脸 1→0 —— 输入变大后置信度非单调，单尺寸无法同时照顾大小脸；
//	640∪1280 并集（IoU 去重后）：约 83 张（兼得两者），大图像检测耗时 ≈ 3.5×。
//
// 因此默认策略（FACES_MULTISCALE，默认开）：图源长边 >640 的图跑两个尺度
// （自适应尺寸 ∪ 640），跨尺度 IoU 去重合并；≤640 的小图与小模型输入（静态 640）
// 完全保持单尺度旧行为。扫描是后台批处理，3.5× 检测耗时换 +1/3 合影召回是赚的；
// 确有性能顾虑的环境设 FACES_MULTISCALE=false 即整体回退。

// multiSizes 规划本次检测的输入边长列表。
//
// 规则（保守、FailClosed 到旧行为）：
//   - 未开多尺度 / 静态输入模型（只有声明尺寸可用）/ 自适应尺寸 ≤640：
//     一律单尺度，与旧行为逐字节一致。
//   - 开多尺度且动态模型且自适应尺寸 >640：返回 [自适应尺寸, 640] ——
//     两尺度在**各自 letterbox 后映射回同一图源坐标**再合并，语义不变。
//
// 顺序：大尺度在前（合并按 conf 贪心，顺序本身不影响结果，只为稳定日志）。
func multiSizes(adaptive, staticSize int, multiScale bool) []int {
	if !multiScale || staticSize > 0 || adaptive <= DefaultInputSize {
		return []int{adaptive}
	}
	return []int{adaptive, DefaultInputSize}
}

// crossScaleIoU 跨尺度去重阈值：同一脸在两尺度下的框因 letterbox 映射存在
// 轻微几何差（边缘 padding 不同），阈值比检测器内部 NMS（0.3）放宽到 0.4 ——
// 低于 0.4 基本不可能是同一脸（两尺度都映射回源坐标，同一脸的框应高度重叠）。
const crossScaleIoU = 0.4

// mergeScales 跨尺度贪心去重：全部检出按 conf 降序，取一个则压制**所有尺度中**
// 与之 IoU > 阈值的框；各尺度独有的小脸/大脸全部保留。
//
// 纯函数（不依赖 ORT），Job000041 的守卫测试以硬编码期望值穷举它。
func mergeScales(byScale [][]Detection) []Detection {
	total := 0
	for _, d := range byScale {
		total += len(d)
	}
	if total == 0 {
		return nil
	}
	// 拍平并记录来源尺度（同一尺度的框已被检测器内部 NMS 去过重，跨尺度才需要这一步）。
	type item struct {
		det   Detection
		scale int
	}
	all := make([]item, 0, total)
	for si, d := range byScale {
		for _, det := range d {
			all = append(all, item{det, si})
		}
	}
	// conf 降序；同 conf 时尺度小者在前（稳定输出，便于测试与对账）。
	for i := 1; i < len(all); i++ {
		for j := i; j > 0; j-- {
			better := all[j].det.Score > all[j-1].det.Score ||
				(all[j].det.Score == all[j-1].det.Score && all[j].scale < all[j-1].scale)
			if !better {
				break
			}
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	kept := make([]Detection, 0, len(all))
	for _, cur := range all {
		dup := false
		for _, k := range kept {
			if iou(cur.det, k) > crossScaleIoU {
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, cur.det)
		}
	}
	return kept
}
