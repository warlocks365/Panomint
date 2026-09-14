// Package tags 标签体系（Phase 4）：AI 零样本自动打标 + EXIF/GPS 启发式兜底。
//
// 设计要点：
//   - AI 打标复用既有 CLIP 向量基础设施（internal/embed）：**不复算图像向量**，
//     只把候选词表编码为 512 维文本向量并缓存，再与 media.embedding 做余弦相似度；
//   - 词表按「场景 / 物体 / 事件」三类组织，同组互斥（如时段、天气、动物），
//     避免一张图被同义标签刷屏；
//   - CLIP 建议以 origin='ai' + confirmed=false 落库，必须经人工确认才计入正式标签；
//   - 启发式（GPS→城市、is_360→全景）来自可信元数据，直接 confirmed=true、origin='heuristic'。
//
// 本文件只定义词表数据，不含推理与 DB 逻辑。
package tags

// TagDef 单个候选标签。
type TagDef struct {
	Label string // 中文标签名（落库名，chinese-clip 族使用）
	EN    string // 英文对照（clip 族使用；英文族的文本塔不认中文提示词）
	Group string // 互斥组名（同组只保留相似度最高者）；空 = 不互斥
}

// Class 词表分类（仅用于组织与展示，不参与阈值判定）。
type Class struct {
	Name string // 场景 | 物体 | 事件
	Tags []TagDef
}

// Vocab 完整词表。
type Vocab struct {
	Classes []Class
}

// DefaultVocab 返回内置中文标签种子词表（108 个，分三类 + 互斥分组）。
//
// 互斥组说明（Group 非空 = 同组只保留相似度最高者）：
//
//	时段   日出/日落/黄昏/夜晚/清晨/夜景
//	天气   晴天/多云/阴天/雨天/雪天/雾天
//	季节   春天/夏天/秋天/冬天
//	室内外 室内/室外
//	动物   猫/狗/鸟/鱼/马/羊/熊猫/蝴蝶
//	交通   汽车/火车/飞机/船/自行车/摩托车/公交车
//	人物   人像/合影/儿童/老人
//	食饮   美食/咖啡/蛋糕/水果
//	地貌   城市/乡村/山地/森林/湖/海滩/沙漠/草原/雪原/雪山/河流
//
// 词表治理记录（114 → 108）：依据是**逐张打开图片目视核对**的实测准确率（准确率 = 判「确实有该事物」/ 核对张数），
// 不是相似度阈值推断。删除 6 个低精度标签（准确率 ≤29%），改名 3 个「名实不符」的标签：
//
//	彩虹   0/12   12 张全是彩条/渐变合成测试图，无一真实彩虹。
//	旗帜   0/14   合成图 ×10、足球场俯拍、林肯肖像、南北战争士兵照，均非旗帜。
//	霓虹灯 0/10   合成图 ×7、会安夜市灯串、帝国大厦夜景、锈蚀老爷车；模型把「夜景灯光」当霓虹。
//	展览   1/11   人群、街景、瀑布、肖像、天际线；唯一命中是店铺陈列内景。
//	潜水   0/2    海浪特写、栈道俯拍。
//	天际线 2/7    误命中集中在人群密集图（游行、矿山/栈桥人群）；真命中仅曼哈顿、巴黎。
//
// 一个 10 次里有 7 次错的标签对用户是负价值，故整条删除而非调阈值：
//
//	冰川 → 雪山   EN glacier → snow mountain
//	      被误标「冰川」的 3 张画面里确实有雪山（极光+雪山、雪山湖泊倒影），改名为「雪山」后 0% → 准确。
//	云海 → 云雾   EN sea of clouds → mist
//	      存疑样本实质是「山间云雾/浓雾」，「云海」这个说法过窄。
//	冲浪 → 海浪   EN surfing → ocean waves
//	      命中样本全是**海浪**特写，模型从不识别冲浪者，改名后才与模型实际能识别的东西对齐。
//
// ⚠️ Label 是落库名：删除/改名会让同名旧标签成为无引用的孤儿行，需在部署后清理（见发布说明）。
//
// 词表治理记录（118 → 114）：删除项均为**非画面语义**或**同义重复**，不是画面概念，删掉只减误命中：
//
//	全景   拍摄几何属性；由启发式 is_360 以置信度 1.0 确定性产出。
//	       作为文本标签会命中任何广阔视野的风景照（实测西湖 360 全景误命中即此类），是纯噪声源。
//	视频   文件类型，单帧画面不可辨识；由启发式 type=video 确定性产出。
//	烟花秀 与「物体」类的「烟花」同义重复，同一张烟花照会同时命中两者。
//	宠物   抽象上位词，且被错误归入「事件」类；宠物照已由「动物」组的猫/狗/鸟/鱼等覆盖。
//
// 另调整两处分组（不改标签名，只改互斥关系）：
//
//	彩虹   移出「天气」组 —— 彩虹是光学现象，天然与晴天/多云**同时存在**，
//	       放进互斥组会让彩虹抢走本应属于正确天气标签的槽位。改后「晴天 + 彩虹」可并存。
//	夜景   并入「时段」组 —— 与「夜晚」同义重复，会让同一张照片同时拿到两个语义相同的标签。
//
// 词表可增删：新增项建议给出 Group，避免与既有标签语义重叠。
func DefaultVocab() *Vocab {
	return &Vocab{Classes: []Class{
		{Name: "场景", Tags: []TagDef{
			{Label: "日出", EN: "sunrise", Group: "时段"},
			{Label: "日落", EN: "sunset", Group: "时段"},
			{Label: "黄昏", EN: "dusk", Group: "时段"},
			{Label: "夜晚", EN: "night", Group: "时段"},
			{Label: "清晨", EN: "early morning", Group: "时段"},
			{Label: "晴天", EN: "clear sky", Group: "天气"},
			{Label: "多云", EN: "cloudy sky", Group: "天气"},
			{Label: "阴天", EN: "overcast", Group: "天气"},
			{Label: "雨天", EN: "rain", Group: "天气"},
			{Label: "雪天", EN: "snowfall", Group: "天气"},
			{Label: "雾天", EN: "fog", Group: "天气"},
			{Label: "春天", EN: "spring season", Group: "季节"},
			{Label: "夏天", EN: "summer season", Group: "季节"},
			{Label: "秋天", EN: "autumn season", Group: "季节"},
			{Label: "冬天", EN: "winter season", Group: "季节"},
			{Label: "室内", EN: "indoor scene", Group: "室内外"},
			{Label: "室外", EN: "outdoor scene", Group: "室内外"},
			{Label: "天空", EN: "sky", Group: ""},
			{Label: "云雾", EN: "mist", Group: ""},
			{Label: "星空", EN: "starry night sky", Group: ""},
			{Label: "极光", EN: "aurora", Group: ""},
			{Label: "城市", EN: "city", Group: "地貌"},
			{Label: "乡村", EN: "countryside", Group: "地貌"},
			{Label: "山地", EN: "mountain", Group: "地貌"},
			{Label: "森林", EN: "forest", Group: "地貌"},
			{Label: "湖", EN: "lake", Group: "地貌"},
			{Label: "海滩", EN: "beach", Group: "地貌"},
			{Label: "沙漠", EN: "desert", Group: "地貌"},
			{Label: "草原", EN: "grassland", Group: "地貌"},
			{Label: "雪原", EN: "snowfield", Group: "地貌"},
			{Label: "雪山", EN: "snow mountain", Group: "地貌"},
			{Label: "河流", EN: "river", Group: "地貌"},
			{Label: "瀑布", EN: "waterfall", Group: ""},
			{Label: "峡谷", EN: "canyon", Group: ""},
			{Label: "田野", EN: "farm field", Group: ""},
			{Label: "花园", EN: "garden", Group: ""},
			{Label: "街景", EN: "street scene", Group: ""},
			{Label: "建筑", EN: "architecture", Group: ""},
			{Label: "古镇", EN: "old town", Group: ""},
			{Label: "寺庙", EN: "temple", Group: ""},
			{Label: "教堂", EN: "church", Group: ""},
			{Label: "城堡", EN: "castle", Group: ""},
			{Label: "桥梁", EN: "bridge", Group: ""},
			{Label: "公园", EN: "park", Group: ""},
			{Label: "港口", EN: "harbor", Group: ""},
			// 与「时段」组的「夜晚」同义重复，并入同一互斥组后每图只保留最高分的那个。
			{Label: "夜景", EN: "night scene", Group: "时段"},
		}},
		{Name: "物体", Tags: []TagDef{
			{Label: "猫", EN: "cat", Group: "动物"},
			{Label: "狗", EN: "dog", Group: "动物"},
			{Label: "鸟", EN: "bird", Group: "动物"},
			{Label: "鱼", EN: "fish", Group: "动物"},
			{Label: "马", EN: "horse", Group: "动物"},
			{Label: "羊", EN: "sheep", Group: "动物"},
			{Label: "熊猫", EN: "giant panda", Group: "动物"},
			{Label: "蝴蝶", EN: "butterfly", Group: "动物"},
			{Label: "汽车", EN: "car", Group: "交通"},
			{Label: "火车", EN: "train", Group: "交通"},
			{Label: "飞机", EN: "airplane", Group: "交通"},
			{Label: "船", EN: "boat", Group: "交通"},
			{Label: "自行车", EN: "bicycle", Group: "交通"},
			{Label: "摩托车", EN: "motorcycle", Group: "交通"},
			{Label: "公交车", EN: "bus", Group: "交通"},
			{Label: "人像", EN: "portrait of a person", Group: "人物"},
			{Label: "合影", EN: "group photo", Group: "人物"},
			{Label: "儿童", EN: "child", Group: "人物"},
			{Label: "老人", EN: "elderly person", Group: "人物"},
			{Label: "美食", EN: "food dish", Group: "食饮"},
			{Label: "咖啡", EN: "coffee", Group: "食饮"},
			{Label: "蛋糕", EN: "cake", Group: "食饮"},
			{Label: "水果", EN: "fruit", Group: "食饮"},
			{Label: "花", EN: "flower", Group: ""},
			{Label: "树", EN: "tree", Group: ""},
			{Label: "雪", EN: "snow", Group: ""},
			{Label: "烟花", EN: "fireworks", Group: ""},
			{Label: "雕塑", EN: "statue", Group: ""},
			{Label: "壁画", EN: "mural", Group: ""},
			{Label: "书本", EN: "book", Group: ""},
			{Label: "电脑", EN: "computer", Group: ""},
			{Label: "手机", EN: "smartphone", Group: ""},
			{Label: "乐器", EN: "musical instrument", Group: ""},
			{Label: "吉他", EN: "guitar", Group: ""},
			{Label: "帐篷", EN: "tent", Group: ""},
			{Label: "篝火", EN: "campfire", Group: ""},
			{Label: "灯笼", EN: "lantern", Group: ""},
			{Label: "礼物", EN: "gift", Group: ""},
			{Label: "气球", EN: "balloon", Group: ""},
			{Label: "长椅", EN: "bench", Group: ""},
			{Label: "楼梯", EN: "staircase", Group: ""},
			{Label: "喷泉", EN: "fountain", Group: ""},
			{Label: "摩天轮", EN: "ferris wheel", Group: ""},
			{Label: "路牌", EN: "street sign", Group: ""},
		}},
		{Name: "事件", Tags: []TagDef{
			{Label: "婚礼", EN: "wedding", Group: ""},
			{Label: "生日", EN: "birthday party", Group: ""},
			{Label: "聚会", EN: "party gathering", Group: ""},
			{Label: "毕业", EN: "graduation", Group: ""},
			{Label: "音乐会", EN: "concert", Group: ""},
			{Label: "运动", EN: "sports", Group: ""},
			{Label: "登山", EN: "hiking", Group: ""},
			{Label: "露营", EN: "camping", Group: ""},
			{Label: "野餐", EN: "picnic", Group: ""},
			{Label: "旅行", EN: "travel trip", Group: ""},
			{Label: "会议", EN: "meeting", Group: ""},
			{Label: "演出", EN: "stage performance", Group: ""},
			{Label: "滑雪", EN: "skiing", Group: ""},
			{Label: "海浪", EN: "ocean waves", Group: ""},
			{Label: "垂钓", EN: "fishing", Group: ""},
			{Label: "骑行", EN: "cycling", Group: ""},
			{Label: "节日", EN: "festival", Group: ""},
			{Label: "自拍", EN: "selfie", Group: ""},
		}},
	}}
}

// LabelCount 词表标签总数（含各类）。
func (v *Vocab) LabelCount() int {
	n := 0
	for _, c := range v.Classes {
		n += len(c.Tags)
	}
	return n
}
