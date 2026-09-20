// Package faces 人脸检测 / 对齐 / 特征 / 聚类（ONNX Runtime，CGO 绑定）。
//
// 模型选型（**均为 Apache-2.0，OpenCV Zoo**，规避 insightface 预训练模型的商用许可风险）：
//
//	检测 YuNet  face_detection_yunet_2023mar.onnx   输入 1×3×H×W float32 BGR，0..255 不归一化
//	特征 SFace  face_recognition_sface_2021dec.onnx 输入 1×3×112×112 float32 RGB，0..255，输出 128 维
//
// 两处预处理参数**直接对齐 OpenCV 参考实现**（不是想当然）：
//   - YuNet ：OpenCV FaceDetectorYN 对 BGR 帧直接送网，不做归一化 → 本包 letterbox 后保持 BGR / 0..255。
//   - SFace ：OpenCV FaceRecognizerSFImpl::feature 为
//     blobFromImage(aligned, 1, Size(112,112), Scalar(0,0,0), swapRB=true, crop=false)
//     → **scale=1、mean=0、通道由 BGR 交换为 RGB**；本包源图本就是 RGB，故等价于直接送原始 0..255 RGB。
//
// 推理设备（GPU / CPU 双接口）由 ortx 提供，配置选择而非编译期裁剪；
// 未启用 CGO 的构建由 faces_nocgo.go 提供占位实现（能力降级，不裁剪接口）。
package faces

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"panoalbum/internal/ortx"
)

// EmbeddingDim SFace 输出维度（与 faces.embedding VECTOR(128) 对齐）。
const EmbeddingDim = 128

// FaceSize SFace 输入边长（112×112，ArcFace 标准对齐尺寸）。
const FaceSize = 112

// LGWidth LG 缩略图宽度（`ffmpeg scale=1280:-2`，宽固定 1280，见 internal/ffmpeg/presets.go）。
//
// faces.bbox 与命名迁移 IoU 的**统一坐标系**（P0-02）：无论当次检测喂的是原图还是
// LG 缩略图，scanOne 入库前一律把 Detection 归一到该坐标系（Detection.Scaled），
// 否则两次扫描图源分辨率不同时 BestFaceMatch 的 IoU 跨坐标系比较 ≈ 0，用户命名静默丢失。
// MinFacePx 的判定同样标定在该坐标系（见 detect.go 的换算）。
const LGWidth = 1280

// 默认参数。阈值需在自有语料上标定（见 Options 注释）。
const (
	// DefaultInputSize 检测输入边长**下限 / 兜底值**。
	//
	// 三种角色，缺一不可（见 yunet.go resolveInputSize）：
	//   - 模型声明静态输入时不用它（静态值优先）；
	//   - 模型输入为动态时的**下限**，也在**拿不到源图尺寸**（如构造期）时充当默认值；
	//   - 源图长边 ≤640 时推导结果即它 → 小图行为与改造前完全一致。
	DefaultInputSize = 640
	// DefaultInputMax 自适应检测输入边长的**上限**（可用 FACE_INPUT_MAX 覆盖，设 640 即退化回旧行为）。
	//
	// # 动机（实测定位）
	//
	// 检测输入过去被钉死在 640×640，而人脸来源 LG 缩略图是 `ffmpeg scale=1280:-2`
	// （宽固定 1280，见 internal/ffmpeg/presets.go:19）——1280 宽的图被 letterbox
	// 压进 640×640，**一半线性分辨率根本没进网络**，大合影里每张脸再砍半 →
	// 必然漏检。这就是检测召回瓶颈的物理机制。
	//
	// # 自适应规则
	//
	// 未显式指定 Options.InputSize 时取
	// `size = clamp(roundUp32(源图长边), DefaultInputSize, DefaultInputMax)`，
	// 于是 1280×791 的合影直接以 1280×1280 画布 **1:1** 送入，不再降采样。
	// 边长向上取 32 的整数倍：YuNet 的三个输出头 stride 为 8/16/32，锚点数按
	// (S/stride)² 算，只有 32 的倍数才同时整除（见 yunet.go inputAlign）。
	//
	// ⚠️ **本机制只在模型输入为动态时生效。** 仓内现役
	// face_detection_yunet_2023mar.onnx 的 graph input 是**静态 [1,3,640,640]**，
	// 12 个输出头的锚点数也写死（cls_8=[1,6400,1]、cls_16=[1,1600,1]、cls_32=[1,400,1]，
	// 其中 6400=(640/8)²）——ONNX Runtime 会拒绝任何其它边长。
	// 故 staticSize>0 时 resolveInputSize 直接返回模型声明值，本常量对它不产生作用；
	// 想吃到这份分辨率必须换成**动态输入**导出（graph input 为 [1,3,height,width]、
	// 输出末维为符号 anchors），例如上游 opencv_zoo 的 face_detection_yunet_2026may.onnx
	// （Apache-2.0，sha256 ebafce4e3c118d6554634be5c27ab333b4c047a9a8c3faf1d7cf93101c22f0f0，
	// 文件名已列在 yuNetCandidates；注意 2023mar 排在前，需先移走旧文件才会被选中）。
	//
	// # 实测漏检数字（2023mar 静态 640 + conf 0.87 + minpx 24，坐标均为 LG 缩略图）
	//
	//	corpus-02-fotograf-a-dos-inaguraci-n-grupal.jpg（1280×791，约 50 人分 4 排）→ **仅 1 张**
	//	    ↑ 唯一命中的那张脸短边只有 25.1px，压到 640 画布后只剩 12.5px（逼近 YuNet 极限），
	//	      其余 49 张更小 → 全部低于可检尺度
	//	corpus-taiwan-military-academy-graduation.jpg（1280×853，≥60 人）→ 30 张
	//	corpus-klassebilde-christies-skole-1903.jpg  （1280×823，17 人）  → 16 张（本来就好）
	//	7 张单人特写                                                     → 各 1 张（正常）
	//
	// 注意：本机制**不改变 minpx 的坐标系**——minpx 始终判定在 LG 缩略图（源图）坐标，
	// 由同一次实际使用的 size 推导出的 letterbox 反向映射保证（见 detect.go Detect）。
	//
	// # 换成动态模型后的实测收益与代价（2026may + 本机 CPU ORT 1.18.1 离线复算）
	//
	// 同一批语料，conf 0.87 / nms 0.30 / minpx 24（LG 坐标）：
	//
	//	文件                              640          1280
	//	corpus-1-foto-bersama（≈30 人）     11           23
	//	corpus-taiwan（≥60 人）             30           33
	//	corpus-02（≈50 人）                  1            3
	//	corpus-atlantic（≥100 人）           0            2
	//	corpus-klassebilde（17 人）         16           16（已饱和）
	//	7 张单人特写                        各 1       6 张仍为 1，**plueschow 侧脸 1→0**
	//
	// ⚠️ 两点必须知道（否则会误判收益）：
	//  1. **大脸会退化**：plueschow（1900s 银盐侧脸，源 1280×1773）在 640 下 conf≥0.87 命中，
	//     到 1280 掉到 0.786 而漏检 —— 输入变大后置信度并非单调上升，单个全局尺寸无法同时
	//     照顾大小脸。要兼得需多尺度（见下）。
	//  2. **上限不是输入尺寸，而是 1280px 源的信息量**：把输入放大到 2560/3200/4096
	//     （即对 1280 源插值放大）后，corpus-02 仍是 1~4 张、taiwan 仍 ~33、atlantic 仍 ~6，
	//     与 1280 基本持平。corpus-02 那 50 张脸在 1280 源里只有约 25px，已贴近 YuNet 可检下限。
	//     → 这类"每张脸仅二十几像素"的合影，**任何检测侧改动都救不回来**，根治只能靠
	//       上游导入更高分辨率的原图（当前库内原图也是 1280 宽）。
	//
	// # 代价
	//
	// 640→1280 是 4 倍像素（1 次推理）；实测 2026may 上 1280/640 单图推理耗时约 2.5×
	// （CPU）。小图（长边≤640）推导结果仍是 640，耗时不变。
	// 1280×1280×3 的 float32 输入张量约 19.7MB（见 detect.go inputCacheMax）。
	DefaultInputMax = 1280
	// DefaultConfThreshold 检测置信度阈值。
	//
	// 0.87 由 Job000011 在 93 条媒体（含 14 张刻意覆盖不同人脸尺度的人群/合影语料）上标定：
	//
	//	唯一误检（霓虹街景横纹误框）       conf 0.861
	//	最小真脸（victoria-2）             conf 0.883
	//	三张合影/人群照的人脸 conf 区间     0.854 ~ 0.940
	//
	// 即「误检 < 最小真脸」在**置信度轴**上可分（窗口 [0.861, 0.883]，取 0.87 居中）。
	// 而在**尺寸轴**上不可分 —— 误检短边 56.7 恰好落在合影真实小脸区间 24.9~73.0 的正中间。
	//
	// ⚠️ 这里踩过一次坑：曾误用尺寸轴（minpx 24→96）来分离误检，结果三张合影的
	// 65/69 张脸被误杀（只剩 5.8%）。教训：**小样本（7 特写 + 1 误检）得不出可分性结论**，
	// 必须用「人脸尺度多样」的语料才能看出误检尺寸会与真脸重叠。
	//
	// 效果对比（93 媒体 / 76 张候选脸，minpx=24 基准）：
	//
	//	conf 0.85 + minpx 96（曾被部署）→ 11 张脸，合影仅 4 张（14%）
	//	conf 0.87 + minpx 24（当前）    → 69 张脸，0 误检，7/7 特写，合影 62 张（91%）
	//
	// ⚠️ 可分窗口较窄（0.022），换语料或换模型时应重新标定。可用 FACE_CONF 覆盖。
	DefaultConfThreshold = 0.87
	// DefaultNMSThreshold 检测 NMS IoU 阈值（对齐 OpenCV 演示默认）。
	DefaultNMSThreshold = 0.3
	// DefaultMinFacePx 最小人脸边长（像素）：过小的人脸特征不可靠，直接丢弃。
	//
	// ⚠️ 判定发生在 **LG 缩略图坐标系**（宽 1280），不是原图 —— 见 detect.go 在
	// MapDetection 之后比较 W/H，而入参本身是缩略图。故该值与缩略图生成规则绑定，
	// 若日后调整 LG 尺寸，此值需按比例重标。
	//
	// 该语义在检测输入边长改成**按源图自适应**（见 DefaultInputMax）后**保持不变**：
	// lb（letterbox 参数）与反向映射都由同一次实际使用的 size 推导，
	// 即"画布坐标 → 源图坐标"的缩放系数恒等于 letterbox 实际缩放系数的倒数，
	// 与 size 取值无关（证明与用例见 yunet.go Letterbox 与 faces_test.go）。
	//
	// 取 24（几乎不设限）是**有意的**：Job000011 用「人脸尺度多样」的语料证明，
	// 在尺寸轴上误检与真脸**不可分** —— 霓虹街景误检短边 56.7，
	// 而合影照片里真实小脸的短边低至 24.9，两者区间重叠。
	// 因此**不要用 minpx 来分离误检**（误检已由 conf 0.87 解决）。
	//
	// 提高 minpx 的实测代价（93 媒体，conf 0.87，76 张候选脸）：
	//
	//	minpx 24 → 69 张脸（合影 62）   minpx 56 → 33 张（合影 25）
	//	minpx 48 → 54 张（合影 47）     minpx 96 → 11 张（合影 4）  ← 曾误部署，毁掉 91% 的合影人脸
	//
	// 该值仍可用于压制「明显过小的纹理噪声」，但**不应**用作物种分离手段。可用 FACE_MIN_PX 覆盖。
	DefaultMinFacePx = 24
	// DefaultMergeSimilarity 聚类合并阈值（余弦相似度）。
	//
	// 取 0.40，由 Job000011 在 93 媒体 / 69 张脸语料上标定，两侧各留余量：
	//
	//	跨照片错并（klasse 脸并进 taiwan 簇）最高相似度  0.347 ~ 0.348  → 需 > 0.348 才能挡住
	//	真同人最低相似度（einstein-1 ↔ einstein-3）      0.4382          → 需 < 0.4382 才能保住
	//
	// 0.40 居中：距错并侧 0.052、距真同人侧 0.038。官方同人判定 0.363 落在错误区间内，
	// **不能直接采用**——SFace 在 112×112 输入下的判别力上限就在此处。
	//
	// ⚠️ **分辨率不是根因，不要走放大输入这条路**：把脸从 60px 提到 217px，
	// 异人最大相似度仅从 0.525 降到 0.4999，检出数/置信度一张不变 → 是模型判别力上限。
	//
	// 剩余重叠无法靠单一阈值解决（异人 0.525 > 真同人 0.4382），
	// 真正的兜底是「同媒体内人脸互不合并」这条强先验（见 store.go NearestClusters）。
	// 可用 FACE_MERGE_SIM 覆盖。
	DefaultMergeSimilarity = 0.40
)

// 模型文件名候选（按顺序取第一个存在的文件）。
//
// ⚠️ 顺序即部署策略：2026may（动态输入）**优先** —— 多尺度与自适应输入都只在动态
// 模型下生效（见 multiscale.go / detect.go 的静态判定），2023mar 仅作存量资产的
// 兼容回退（其静态 [1,3,640,640] 输入无法服务其它边长）。
var (
	yuNetCandidates = []string{
		"face_detection_yunet_2026may.onnx",
		"face_detection_yunet_2023mar.onnx",
		"face_detection_yunet.onnx",
	}
	sFaceCandidates = []string{
		"face_recognition_sface_2021dec.onnx",
		"face_recognition_sface.onnx",
	}
)

// ErrCGORequired 表示当前构建未启用 CGO，人脸推理不可用。
var ErrCGORequired = errors.New("本构建未启用 CGO，人脸识别能力不可用（需 CGO_ENABLED=1 重新构建）")

// Detection 一张人脸（坐标均为**当次检测图源**像素坐标）。
//
// ⚠️ 图源可能是原图也可能是 LG 缩略图（见 LoadScanImage），两者分辨率不同。
// 入库（SaveFace）与命名迁移（BestFaceMatch）前必须先用 Scaled 归一到 LG/1280
// 坐标系（faces.LGWidth），对齐已存量的 bbox 语义；特征对齐（EmbedFace）则用
// 未归一的图源坐标（与图源本身一致）。
type Detection struct {
	X, Y, W, H float64
	// Landmarks 五点：右眼、左眼、鼻尖、右嘴角、左嘴角（YuNet / OpenCV 约定）。
	Landmarks [5][2]float64
	Score     float64
}

// Scaled 返回按系数 f 等比缩放后的检出（框与五点一起缩放；f==1 时原样返回）。
//
// 典型用法是把「当次图源坐标」归一到 LG/1280 坐标系：f = float64(LGWidth) / 图源宽
// （ScanSource.Scale 已由 LoadScanImage 算好）。
func (d Detection) Scaled(f float64) Detection {
	if f == 1 {
		return d
	}
	d.X, d.Y, d.W, d.H = d.X*f, d.Y*f, d.W*f, d.H*f
	for i := range d.Landmarks {
		d.Landmarks[i][0] *= f
		d.Landmarks[i][1] *= f
	}
	return d
}

// Options 人脸流水线配置。
type Options struct {
	ModelDir string // 含 YuNet / SFace 两个 onnx 的目录
	LibPath  string // libonnxruntime 路径；空则按平台名走系统搜索

	// 推理设备（cpu|cuda|auto）；GPU / CPU 双接口保留，不因开发机无 CUDA 而裁剪。
	Device        ortx.DeviceKind
	DeviceID      int
	IntraThreads  int
	GpuMemLimitMB int

	InputSize     int     // 检测输入边长（0=按源图长边自适应，见 DefaultInputMax；显式值优先于一切推导）
	InputMax      int     // 自适应输入边长上限（0=DefaultInputMax 1280）；FACE_INPUT_MAX，设 640 即退化回旧行为
	ConfThreshold float64 // 检测置信度阈值（0=默认 0.9）
	NMSThreshold  float64 // 检测 NMS IoU 阈值（0=默认 0.3）
	MinFacePx     int     // 最小人脸边长（0=默认 24）
	MergeSim      float64 // 聚类合并余弦阈值（0=默认 0.40）
	// MultiScale 多尺度检测（Job000041）：长边>640 的图跑「自适应尺寸 ∪ 640」两尺度
	// 并跨尺度 IoU 去重，兼得小脸与大脸（benchmark 见 multiscale.go 头注释）。
	// 环境变量 FACES_MULTISCALE（空=默认开；false/0=关）；静态输入模型自动退化为单尺度。
	MultiScale bool

	// MediaRoot 原图根目录：MediaItem.Path 相对它解析。空 = 不使用原图，直接用 LG 缩略图。
	//
	// 为什么要有它：检测输入边长按源图长边自适应（resolveInputSize），而 LG 缩略图宽固定 1280 ——
	// 喂原图才能吃到"输入更大 → 更小的人脸也能检出"的收益。但原图可能不存在
	// （只导入过缩略图 / NAS 未挂载 / 源文件已移除），故它只是首选，回退链见 LoadScanImage。
	MediaRoot string

	// Store 聚类所需的 DB 存取（nil=只检测不聚类）。
	Store *Store
}

// OptionsFromEnv 从环境变量构造配置（flag 可再覆盖）。
//
//	FACE_MODEL_DIR   模型目录（默认 assets/models/faces）
//	FACE_LIB / EMBED_LIB  onnxruntime 原生库路径
//	FACE_DEVICE      cpu|cuda|auto（默认 auto）
//	FACE_DEVICE_ID / FACE_THREADS / FACE_GPU_MEM_MB
//	FACE_INPUT_SIZE / FACE_CONF / FACE_NMS / FACE_MIN_PX / FACE_MERGE_SIM
//	FACE_INPUT_MAX   自适应检测输入边长上限（默认 1280；设 640 完全退化回旧行为）
//	FACES_MULTISCALE 多尺度检测（空=默认开；false/0=关），仅动态输入模型生效
func OptionsFromEnv() Options {
	lib := os.Getenv("FACE_LIB")
	if lib == "" {
		lib = os.Getenv("EMBED_LIB") // 与语义向量共用同一份 ORT 运行时
	}
	o := Options{
		ModelDir: ModelDirFromEnv(),
		LibPath:  lib,
		Device:   ortx.ParseDevice(firstNonEmpty(os.Getenv("FACE_DEVICE"), os.Getenv("EMBED_DEVICE"))),
	}
	o.DeviceID = atoi(os.Getenv("FACE_DEVICE_ID"))
	o.IntraThreads = atoi(firstNonEmpty(os.Getenv("FACE_THREADS"), os.Getenv("EMBED_THREADS")))
	o.GpuMemLimitMB = atoi(firstNonEmpty(os.Getenv("FACE_GPU_MEM_MB"), os.Getenv("EMBED_GPU_MEM_MB")))
	o.InputSize = atoi(os.Getenv("FACE_INPUT_SIZE"))
	o.InputMax = atoi(os.Getenv("FACE_INPUT_MAX"))
	o.ConfThreshold = atof(os.Getenv("FACE_CONF"))
	o.NMSThreshold = atof(os.Getenv("FACE_NMS"))
	o.MinFacePx = atoi(os.Getenv("FACE_MIN_PX"))
	o.MergeSim = atof(os.Getenv("FACE_MERGE_SIM"))
	// 原图根目录：与索引/缩略图共用同一个 MEDIA_ROOT（回退链见 LoadScanImage）。
	// 未设置 = 不使用原图，扫描退化为只用 LG 缩略图（即旧行为）。
	o.MediaRoot = os.Getenv("MEDIA_ROOT")
	// 多尺度默认开（Job000041 benchmark 裁决：+32% 合影召回，后台扫描耗时可接受）；
	// 显式 false/0 才关 —— 空串与"没设过"无法区分，布尔只能这样表达"默认开"。
	o.MultiScale = parseBoolDefaultTrue(os.Getenv("FACES_MULTISCALE"))
	return o
}

// parseBoolDefaultTrue ""/true/1/on/y/yes → true；false/0/off/n/no → false。
// 非法值按 true（默认开），并交由调用方日志关注 —— 静默关功能比静默开更危险。
func parseBoolDefaultTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "off", "n", "no":
		return false
	}
	return true
}

// ModelDirFromEnv 解析模型目录：优先 FACE_MODEL_DIR，其次仓库内默认位置。
func ModelDirFromEnv() string {
	if v := os.Getenv("FACE_MODEL_DIR"); v != "" {
		return v
	}
	return filepath.Join("assets", "models", "faces")
}

// WithDefaults 补齐零值（便于 flag 只覆盖部分字段）。
func (o Options) WithDefaults() Options {
	if o.Device == "" {
		o.Device = ortx.DeviceAuto
	}
	if o.ConfThreshold <= 0 {
		o.ConfThreshold = DefaultConfThreshold
	}
	if o.NMSThreshold <= 0 {
		o.NMSThreshold = DefaultNMSThreshold
	}
	if o.MinFacePx <= 0 {
		o.MinFacePx = DefaultMinFacePx
	}
	if o.MergeSim <= 0 {
		o.MergeSim = DefaultMergeSimilarity
	}
	if o.InputMax <= 0 {
		o.InputMax = DefaultInputMax
	}
	return o
}

// loadModelFile 在模型目录内按候选名找到实际存在的模型文件。
func loadModelFile(dir string, candidates []string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("模型目录为空")
	}
	for _, name := range candidates {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("在 %s 未找到模型文件（候选：%s）", dir, strings.Join(candidates, ", "))
}

// L2Normalize / CosineSimilarity / vectorLiteral / parseVector 已收敛到
// internal/vecutil（审查 P2-06）：向量小工具全仓只有一份实现，调用方按精度策略传参。

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
