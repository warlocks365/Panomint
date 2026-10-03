package geo

// Job000143：拍摄地短地名提取。
//
// 用户裁决（2026-10-03）：从完整地址里取**字符串尾部**的具体地名，
// 而**不是**头部的行政区名。理由：头部是「北京市」这类行政区，对用户
// 定位媒体几乎没有信息量；尾部才是「景山前街」「科技园」这种具体地标，
// 才是用户心里想的「拍摄地」。
//
// 例：
//
//	「北京市东城区景山前街 4 号」   → 「景山前街」
//	「浙江省杭州市西湖区龙井路 1 号」 → 「龙井路」
//	「故宫博物院」                  → 原样返回（无路/号段可剥离）
//
// 算法是**纯字符串处理**，不调用任何地图 API —— 高德 regeo 不返回
// township 级别的短名，要拿到得另调 district 接口（多一次配额消耗）。
// 提取不出来时返回空串，由前端留空让用户手填，**不猜**。

import "strings"

// 详细地址里的行政/地域前缀标记（用于从头剥离）。按出现顺序优先匹配长的，
// 避免「北京市」被「北京」截断后留下「市」。
var addrPrefixTokens = []string{
	"北京市", "上海市", "天津市", "重庆市",
	"香港特别行政区", "澳门特别行政区",
	"黑龙江省", "内蒙古自治区", "新疆维吾尔自治区", "西藏自治区",
	"宁夏回族自治区", "广西壮族自治区",
	"辽宁省", "吉林省", "浙江省", "江苏省", "山东省", "山西省",
	"河北省", "河南省", "陕西省", "甘肃省", "青海省", "海南省",
	"四川省", "贵州省", "云南省", "湖南省", "湖北省", "安徽省",
	"江西省", "福建省", "台湾省", "广东省", "广西省",
	"北京市", "上海市", "天津市", "重庆市",
	"北京", "上海", "天津", "重庆",
	"河北", "山西", "辽宁", "吉林", "江苏", "浙江", "安徽", "福建",
	"江西", "山东", "河南", "湖北", "湖南", "广东", "海南", "四川",
	"贵州", "云南", "陕西", "甘肃", "青海", "台湾",
	"内蒙古", "新疆", "西藏", "宁夏", "广西", "香港", "澳门",
	// 北京市/上海市/天津/重庆的市辖区（直辖市特例：没有「省」这一层，
	//「北京市东城区」里「北京市」被剥后必须继续剥「东城区」，
	// 否则会得到「东城区景山前街」这种带行政区前缀的半成品 —— 实测踩过）。
	"东城区", "西城区", "朝阳区", "海淀区", "丰台区", "石景山区",
	"通州区", "昌平区", "大兴区", "顺义区", "房山区", "门头沟区",
	"平谷区", "怀柔区", "密云区", "延庆区",
	"浦东新区", "黄浦区", "徐汇区", "静安区", "长宁区", "普陀区",
	"虹口区", "杨浦区", "闵行区", "宝山区", "嘉定区", "松江区",
	"青浦区", "奉贤区", "崇明区",
	"和平区", "河东区", "河西区", "南开区", "河北区", "红桥区",
	"东丽区", "西青区", "津南区", "北辰区", "武清区", "宝坻区",
	"渝中区", "江北区", "沙坪坝区", "渝北区", "九龙坡区", "南岸区",
}

// addrSuffixTokens 地址尾部的字级后缀词，命中处即在词后切分。
// 顺序即优先级：先匹配更长的组合（「胡同」先于「巷」）。
var addrSuffixTokens = []string{
	"胡同", "大街", "大道", "南路", "北路", "东路", "西路", "中路",
	"街", "路", "巷", "弄",
}

// placeStopWords 尾部切分后应剥掉的后缀噪声（如「景区」「公园」不是地点核心）。
// ⚠️ 这里刻意**不剥**「景区/公园/博物馆」等 —— 「故宫博物院」作为拍摄地更有意义，
// 剥了反而丢信息。保留该列表仅为将来需求预留，当前为空。
var placeStopWords = []string{}

// ShortPlaceName 从完整地址提取短地名（取**尾部**具体地标）。
//
// 策略：先从头剥掉行政区前缀（若有），再从尾部找路/街类后缀词，
// 在该词**之后**切分并去掉门牌号。都不命中则原样返回去空白后的地址，
// 交由用户确认或手填。
//
// 返回空串表示「无法提取出有意义的短地名」，调用方应让用户手填而非填占位值。
func ShortPlaceName(address string) string {
	s := strings.TrimSpace(address)
	if s == "" {
		return ""
	}

	// 1) 剥头部行政区前缀（可能多层：「浙江省杭州市西湖区」）
	head := s
	for {
		before := head
		head = stripAdminPrefix(head)
		if head == before {
			break
		}
		if head == "" {
			return "" // 剥空说明整个串都是行政区，不构成地名
		}
	}

	// 2) 从尾部找路/街类后缀词，在其后切分
	for _, suf := range addrSuffixTokens {
		if i := strings.LastIndex(head, suf); i >= 0 {
			cand := head[:i+len(suf)]
			cand = stripHouseNumber(cand)
			cand = strings.TrimSpace(cand)
			if cand != "" && isMeaningfulPlace(cand) {
				return cand
			}
		}
	}

	// 3) 未命中：剥掉尾部门牌号后原样返回（如「故宫博物院」）
	cand := stripHouseNumber(head)
	cand = strings.TrimSpace(cand)
	if cand == "" {
		return ""
	}
	// 太长的（>24 字）多半是没剥干净的完整地址，视为提取失败交用户手填。
	if len([]rune(cand)) > 24 {
		return ""
	}
	return cand
}

// stripAdminPrefix 剥掉串首的一个行政区前缀，返回剩余部分。
//
// 两条互补策略（先精确 token，后通用后缀），缺一不可：
//  1. addrPrefixTokens 精确表 —— 覆盖「北京市」「香港特别行政区」这类不以后缀
//     收尾的名字，以及「朝阳区」这种**直辖市市辖区**（它们带「区」后缀，
//     但「东城区」的「区」是名字的一部分，通用规则剥不掉，靠精确表）。
//  2. 通用后缀规则 —— 遇到以「省/市/区/县/镇/乡」收尾的最长前缀就剥，
//     覆盖全国两千余个县级单位（不可能全枚举）。
//
// 无剥离发生时返回原串（调用方靠前后比较判断是否终止）。
func stripAdminPrefix(s string) string {
	rest := s
	// 策略 1：精确表
	for _, tok := range addrPrefixTokens {
		if strings.HasPrefix(rest, tok) {
			return rest[len(tok):]
		}
	}
	// 策略 2：通用后缀 —— 找**最后一个**作为行政区后缀的字符，剥掉它之前的所有内容。
	// 用 rune 遍历（range 对 string 给出的是字节索引，中文是 3 字节，直接切会切坏）。
	// 取「最后一个」而非「第一个」：「北京市东城区」的最后一个「区」之前
	// 整体是行政区，剥一次即可；「南京市区」则只剥到「南京」。
	runes := []rune(rest)
	cut := -1
	for i, r := range runes {
		switch r {
		case '省', '市', '区', '县', '镇', '乡':
			// 后面至少留 2 个字符，否则说明前缀吃掉了整个串（如「北京市」）
			if len(runes)-i > 2 {
				cut = i + 1
			}
		}
	}
	if cut > 0 {
		return string(runes[cut:])
	}
	return rest
}

// stripHouseNumber 去掉尾部门牌部分：「景山前街 4 号」→「景山前街」。
// 覆盖中英文数字与常见连接符（- / 之）。
func stripHouseNumber(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	// 找最后一个「号」字，其后（及含空格的紧前）若无实义内容则截断
	i := strings.LastIndex(t, "号")
	if i < 0 {
		return t
	}
	// 「号」前只允许是数字/字母/连字符，否则「号」是地名的一部分
	//（如「金三里 3 号院」中的「号院」不是门牌）
	prefix := t[:i]
	last := rune(prefix[len(prefix)-1])
	if last >= '0' && last <= '9' || last >= 'A' && last <= 'Z' ||
		last >= 'a' && last <= 'z' || last == '-' || last == '／' || last == '/' {
		return strings.TrimRight(strings.TrimSpace(prefix), "-/－ ")
	}
	return t
}

// isMeaningfulPlace 判断切分结果是否像一个地名（而非空壳噪声）。
func isMeaningfulPlace(s string) bool {
	r := []rune(strings.TrimSpace(s))
	if len(r) < 2 {
		return false
	}
	// 剥完前缀后若只剩单个虚词（如「市」「区」），视为无效
	last := r[len(r)-1]
	if strings.ContainsRune("市省区县州镇村", last) && len(r) <= 3 {
		return false
	}
	for _, w := range placeStopWords {
		if s == w {
			return false
		}
	}
	return true
}
