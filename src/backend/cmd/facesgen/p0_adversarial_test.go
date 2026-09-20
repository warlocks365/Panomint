package main

// P0-3 对抗性验证：scanOne 的坐标归一接线形状。
//
// 攻击面：scanOne 若把**未归一**的检出（dets，原图坐标）直接交给 SaveFace 或
// BestFaceMatch，则「LG 旧框 × 原图重扫」IoU≈0，用户命名静默丢失；而 EmbedFace
// 恰恰相反，必须用**图源坐标**的 landmarks（与 img 一致），若误用归一后的坐标，
// 对齐会裁错位置、特征全废。两个方向都钉。

import (
	"os"
	"strings"
	"testing"
)

// scanOneBody 抠出 scanOne 函数体。
func scanOneBody(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读不到 main.go（测试需在 cmd/facesgen 下运行）: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "func scanOne(")
	if start < 0 {
		t.Fatal("main.go 里找不到 scanOne —— 被改名或移走？")
	}
	rest := src[start+1:]
	if next := strings.Index(rest, "\nfunc "); next >= 0 {
		return rest[:next]
	}
	return rest
}

func TestAdversarialScanOneNormalizesBeforeStoreAndMatch(t *testing.T) {
	body := scanOneBody(t)

	// 1) 归一必须存在且逐框进行。
	iScaled := strings.Index(body, "d.Scaled(src.Scale)")
	if iScaled < 0 {
		t.Fatalf("scanOne 找不到 d.Scaled(src.Scale) —— 检出未归一，bbox 坐标系随图源漂移:\n%s", body)
	}

	// 2) 入库与命名迁移必须用归一后的 lgDets，且禁止出现 dets[i] 形态。
	iSave := strings.Index(body, "st.SaveFace(")
	iMatch := strings.Index(body, "faces.BestFaceMatch(")
	if iSave < 0 || iMatch < 0 {
		t.Fatalf("scanOne 缺 SaveFace/BestFaceMatch 调用:\n%s", body)
	}
	if !strings.Contains(body, "st.SaveFace(ctx, m.ID, lgDets[i]") {
		t.Fatalf("SaveFace 的检出参数不是 lgDets[i] —— 未归一的原图坐标直接入库:\n%s", body)
	}
	if !strings.Contains(body, "faces.BestFaceMatch(olds, lgDets[i]") {
		t.Fatalf("BestFaceMatch 的新检出参数不是 lgDets[i] —— 跨坐标系比 IoU，命名静默丢失:\n%s", body)
	}
	if strings.Contains(body, "SaveFace(ctx, m.ID, dets[i]") || strings.Contains(body, "BestFaceMatch(olds, dets[i]") {
		t.Fatal("发现把未归一的 dets[i] 交给 SaveFace/BestFaceMatch 的旁路")
	}

	// 3) 归一必须排在首次使用 lgDets 之前（先归一，后匹配/入库）。
	if iScaled > iMatch || iScaled > iSave {
		t.Fatal("Scaled 归一排在 BestFaceMatch/SaveFace 之后 —— 等于没归一")
	}

	// 4) 反向钉：特征对齐必须用**图源坐标**（d.Landmarks），不得用归一后的。
	if !strings.Contains(body, "rec.EmbedFace(img, d.Landmarks)") {
		t.Fatalf("EmbedFace 的 landmarks 不是图源坐标的 d.Landmarks —— 对齐会裁错位置:\n%s", body)
	}
	if strings.Contains(body, "EmbedFace(img, lgDets[") {
		t.Fatal("EmbedFace 误用了归一后的坐标 —— 归一坐标相对原图是缩小的，对齐全废")
	}

	// 5) 自证：把 SaveFace 换成未归一 dets[i] 的坏版本，上面的断言 2 必须落空。
	broken := strings.Replace(body, "st.SaveFace(ctx, m.ID, lgDets[i]", "st.SaveFace(ctx, m.ID, dets[i]", 1)
	if strings.Contains(broken, "st.SaveFace(ctx, m.ID, lgDets[i]") {
		t.Fatal("守卫失效：SaveFace 改用未归一坐标后断言 2 仍能通过")
	}
}
