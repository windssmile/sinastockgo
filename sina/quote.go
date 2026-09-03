package sina

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Level 是盘口的一档。
type Level struct {
	Price float64
	Vol   float64
}

// KV 保留字段顺序，详情面板按原始顺序展示。
type KV struct{ K, V string }

// Quote 是所有品种的公共视图。各品种独有的字段落在 Fields 里，
// 由详情面板按品种原样展示——A股有五档，期货有持仓量，美股有市盈率，
// 硬塞进一个结构体只会得到一堆永远为空的列。
type Quote struct {
	Code string
	Kind Kind
	Name string

	Last, Prev, Open, High, Low float64
	Volume, Amount              float64
	Date, Time                  string

	// 涨跌额/幅：部分品种直接给，其余由 Last-Prev 算出。
	Change, ChangePct float64

	Bids, Asks []Level // 仅 A 股非空

	Fields []KV // 全部命名字段，含上面已提取的，供详情面板展示
	Raw    string
}

// Kind 是品种大类，决定 CSV 布局和 UI 里的分组。
type Kind string

const (
	KindCN     Kind = "A股"
	KindHK     Kind = "港股"
	KindUS     Kind = "美股"
	KindIntIdx Kind = "国际指数"
	KindFX     Kind = "外汇"
	KindNF     Kind = "国内期货"
	KindHF     Kind = "国外期货"
	KindCrypto Kind = "数字币"
	KindFund   Kind = "场外基金"
	KindUK     Kind = "英股"
	KindBond   Kind = "国债"
	KindMSCI   Kind = "MSCI指数"
)

// layouts 把 CSV 的位置映射成字段名。空串表示该位置含义不明，
// 会以 fN 落进 Fields。这样加一个品种就是加一行表，不是加一个解析函数。
var layouts = map[Kind][]string{
	KindCN: {
		"名称", "今开", "昨收", "现价", "最高", "最低", "竞买价", "竞卖价", "成交量", "成交额",
		"买一量", "买一价", "买二量", "买二价", "买三量", "买三价", "买四量", "买四价", "买五量", "买五价",
		"卖一量", "卖一价", "卖二量", "卖二价", "卖三量", "卖三价", "卖四量", "卖四价", "卖五量", "卖五价",
		"日期", "时间", "状态",
	},
	KindHK: {
		"英文名", "名称", "今开", "昨收", "最高", "最低", "现价", "涨跌额", "涨跌幅",
		"买入价", "卖出价", "成交额", "成交量", "市盈率", "", "52周最高", "52周最低", "日期", "时间",
	},
	KindUS: {
		"名称", "现价", "涨跌幅", "时间", "涨跌额", "今开", "最高", "最低", "52周最高", "52周最低",
		"成交量", "10日均量", "市值", "每股收益", "市盈率", "", "", "", "", "流通股本",
		"", "盘后价", "盘后涨跌额", "盘后涨跌幅", "盘后时间", "收盘时间", "昨收",
	},
	KindIntIdx: {"名称", "现价", "涨跌额", "涨跌幅"},
	KindHF: {
		"现价", "昨收", "买价", "卖价", "最高", "最低", "时间", "昨结算", "今开",
		"持仓量", "买量", "卖量", "日期", "名称",
	},
	// 金融期货（IF/IH/T/TL…），报文以开盘价打头、名称在末列。
	KindNF: {
		"今开", "最高", "最低", "现价", "成交量", "成交额", "持仓量", "买一价", "卖一价",
		"涨停价", "跌停价", "", "", "昨结算", "昨收", "昨持仓", "今结算",
	},
	// 商品期货（RB/AU/CU…）用的是另一套布局：以名称打头，时间是 HHMMSS。
	// 同为 nf_ 前缀却不同构，靠首列是否为数字区分。
	kindNFCommodity: {
		"名称", "时间", "今开", "最高", "最低", "", "买价", "卖价", "现价", "",
		"昨结算", "买量", "卖量", "持仓量", "成交量", "交易所", "品种", "日期",
	},
	KindFund: {"名称", "净值", "累计净值", "昨收", "涨跌幅", "日期"},
	// 列序是 高/开/低/收，很别扭但单看一只股票分不出来——hsba 和 bp. 两个样本
	// 同时代入才只剩这一种自洽解（见 TestUKLayoutNeedsTwoSamples）。
	KindUK: {
		"名称", "现价", "最高", "今开", "最低", "昨收", "成交量", "成交额", "时间",
		"买价", "买量", "卖价", "卖量",
	},
	// ponytail: fx_/btc_ 仍有若干列含义不明（休市时多列取值相同，无法反推）。
	// 只命名数值上能自洽的位置——如 现价-昨收 恰等于 涨跌额——其余留 fN
	// 由详情面板原样展示。升级路径：盘中多次采样比对哪一列在动即可补全。
	KindFX: {
		"时间", "今开", "", "", "", "最低", "最高", "昨收", "现价", "名称",
		"", "", "涨跌额", "说明", "", "", "", "日期",
	},
	KindCrypto: {
		"时间", "买价", "卖价", "现价", "", "", "最高", "最低", "均价", "名称",
		"涨跌额", "日期", "成交量", "成交额", "货币",
	},
	// 各国国债收益率（globalbd_）。这里的「价」是收益率百分数，不是券价——
	// 券价在末尾的「净价」列。报文里的日期/时间一律是纽约时间（德债那条写的
	// 17:06 也是纽约，不是法兰克福当地），入表前按「时间戳」换成北京时间，
	// 原始值留作 纽约日期/纽约时间 备查。
	KindBond: {
		"名称", "今开", "昨收", "现价", "最高", "最低", "", "涨跌幅", "涨跌额", "", "",
		"时间戳", "纽约日期", "纽约时间", "到期日", "票面利率", "付息频率", "净价", "国家",
	},
	// MSCI 指数（msci_STRD_USD_704844 这类），新浪「线索Clues」频道在用，
	// suggest 完全不收录。报文里未命名的列休市时全是常量，反推不出含义；
	// 命名的这几个位置由四只样本互相印证（现价+涨跌额=昨收，且 OHLC 自洽）。
	// 只有日线数据，每日北京时间清晨更新一次，盘中不动。
	KindMSCI: {
		"英文名", "名称", "", "", "现价", "", "", "", "", "",
		"时间", "", "", "", "", "", "涨跌额", "涨跌幅", "", "最高",
		"最低", "今开", "昨收", "", "", "", "", "日期",
	},
}

// kindNFCommodity 只用于选布局，不对外暴露——商品和金融期货在 UI 上同属国内期货。
const kindNFCommodity Kind = "国内期货(商品)"

// NormalizeCode 把用户能拿到的写法转成 hq 认的代码。
// 新浪「线索Clues」页面上 MSCI 指数写作 M.STRD_USD_704844，hq 要 msci_ 前缀。
func NormalizeCode(code string) string {
	code = strings.TrimSpace(code)
	if strings.HasPrefix(code, "M.") {
		return "msci_" + code[2:]
	}
	return code
}

// KindOf 从 sinajs 代码前缀判断品种。
func KindOf(code string) Kind {
	switch {
	case strings.HasPrefix(code, "of"):
		return KindFund
	case strings.HasPrefix(code, "rt_hk"):
		return KindHK
	case strings.HasPrefix(code, "gb_"):
		return KindUS
	case strings.HasPrefix(code, "int_"):
		return KindIntIdx
	case strings.HasPrefix(code, "fx_"):
		return KindFX
	case strings.HasPrefix(code, "nf_"):
		return KindNF
	case strings.HasPrefix(code, "hf_"):
		return KindHF
	case strings.HasPrefix(code, "btc_"):
		return KindCrypto
	case strings.HasPrefix(code, "lse_"):
		return KindUK
	case strings.HasPrefix(code, "globalbd_"):
		return KindBond
	case strings.HasPrefix(code, "msci_"):
		return KindMSCI
	default:
		return KindCN
	}
}

var (
	reDate = regexp.MustCompile(`^\d{4}[-/]\d{2}[-/]\d{2}$`)
	reTime = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}$`)
	reNum  = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

var beijing = time.FixedZone("CST", 8*3600)

func isNumeric(s string) bool { return reNum.MatchString(strings.TrimSpace(s)) }

// Parse 解析一条 `code=csv` 的行情。csv 为空表示该代码无数据。
func Parse(code, csv string) (Quote, bool) {
	if strings.TrimSpace(csv) == "" {
		return Quote{}, false
	}
	q := Quote{Code: code, Kind: KindOf(code), Raw: csv}
	cols := strings.Split(csv, ",")
	layoutKind := q.Kind
	// 商品期货以名称打头，金融期货以开盘价打头——同是 nf_ 却不同构。
	if q.Kind == KindNF && !isNumeric(cols[0]) {
		layoutKind = kindNFCommodity
	}
	layout := layouts[layoutKind]

	num := func(s string) float64 { f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return f }
	get := map[string]string{}

	for i, v := range cols {
		name := ""
		if i < len(layout) {
			name = layout[i]
		}
		if name == "" {
			name = "f" + strconv.Itoa(i)
		} else if _, dup := get[name]; !dup {
			get[name] = v
		}
		q.Fields = append(q.Fields, KV{name, v})
	}

	q.Name = get["名称"]
	q.Last = num(get["现价"])
	if q.Kind == KindFund {
		q.Last = num(get["净值"]) // 场外基金没有盘中价，只有每日净值
	}
	q.Prev = num(get["昨收"])
	q.Open = num(get["今开"])
	q.High = num(get["最高"])
	q.Low = num(get["最低"])
	q.Volume = num(get["成交量"])
	q.Amount = num(get["成交额"])
	q.Date, q.Time = get["日期"], get["时间"]
	// 美股把日期和时间塞在同一列：`2026-08-29 08:14:52`。
	if d, t, ok := strings.Cut(q.Time, " "); ok {
		q.Date, q.Time = d, t
	}
	// 国债给的是纽约时间，和自选里其它品种排在一起没法看。报文另给了 unix 秒，
	// 换算成北京时间。中国不用夏令时，固定 +8 即可，不必依赖系统 tzdata。
	if q.Kind == KindBond {
		if ts := int64(num(get["时间戳"])); ts > 0 {
			t := time.Unix(ts, 0).In(beijing)
			q.Date, q.Time = t.Format(time.DateOnly), t.Format(time.TimeOnly)
		}
	}
	// MSCI 的时间带纳秒（06:02:37.000000000），截到秒。
	if i := strings.IndexByte(q.Time, '.'); i > 0 {
		q.Time = q.Time[:i]
	}
	// 商品期货的时间是无分隔的 HHMMSS（如 023000）。
	if len(q.Time) == 6 && isNumeric(q.Time) {
		q.Time = q.Time[0:2] + ":" + q.Time[2:4] + ":" + q.Time[4:6]
	}

	// nf_ 的尾部字段数随合约变化，名称永远在最后一列；日期/时间靠格式认出来。
	if q.Name == "" && len(cols) > 0 {
		q.Name = strings.TrimSpace(cols[len(cols)-1])
	}
	for _, c := range cols {
		c = strings.TrimSpace(c)
		if q.Date == "" && reDate.MatchString(c) {
			q.Date = c
		}
		if q.Time == "" && reTime.MatchString(c) {
			q.Time = c
		}
	}
	// 期货以昨结算而非昨收为涨跌基准，与新浪网页口径一致。
	if s := num(get["昨结算"]); s != 0 && (q.Kind == KindNF || q.Kind == KindHF || q.Prev == 0) {
		q.Prev = s
	}

	// 集合竞价时 A 股的现价恒为 0，虚拟匹配价只出现在竞买价里——不补上，
	// 现价、涨跌和下面的买卖一档就全是 0。
	if q.Kind == KindCN && q.Last == 0 && inCallAuction(q.Time) {
		q.Last = num(get["竞买价"])
	}

	if v, ok := get["涨跌额"]; ok {
		q.Change = num(v)
	} else if q.Prev != 0 && q.Last != 0 {
		q.Change = q.Last - q.Prev
	}
	// 只给涨跌额不给昨收的品种（如 btc_），反推出基准价，否则涨跌幅永远是 0。
	if q.Prev == 0 && q.Change != 0 {
		q.Prev = q.Last - q.Change
	}
	if v, ok := get["涨跌幅"]; ok {
		q.ChangePct = num(v)
	} else if q.Prev != 0 {
		q.ChangePct = q.Change / q.Prev * 100
	}

	for i := 1; i <= 5; i++ {
		d := []string{"", "一", "二", "三", "四", "五"}[i]
		if p, ok := get["买"+d+"价"]; ok {
			q.Bids = append(q.Bids, Level{num(p), num(get["买"+d+"量"])})
			q.Asks = append(q.Asks, Level{num(get["卖"+d+"价"]), num(get["卖"+d+"量"])})
		}
	}
	// 早盘集合竞价（09:15–09:25）新浪不推买卖一，只给虚拟匹配价（即现价），
	// 买卖一档留空会显示成 0.00，这里补成现价。
	if q.Kind == KindCN && q.Last != 0 && len(q.Bids) > 0 && inCallAuction(q.Time) {
		q.Bids[0].Price = q.Last
		q.Asks[0].Price = q.Last
	}
	return q, true
}

// indexPrefixes 是 A 股各交易所的指数代码段。指数的成交额是成分股汇总、
// 成交量单位也不同，两者相除得不到任何有意义的价格。
var indexPrefixes = []string{"sh000", "sh950", "sz399", "bj899"}

// AvgPrice 返回当日均价（成交额/成交量）。只有 A 股和港股个股的报文里
// 成交额是元、成交量是股，其余品种（期货按手、外汇无量、指数是汇总）
// 相除的结果没有意义，一律返回 0，由 UI 显示成「-」。
func (q Quote) AvgPrice() float64 {
	if q.Volume == 0 || q.Amount == 0 {
		return 0
	}
	if q.Kind != KindCN && q.Kind != KindHK {
		return 0
	}
	for _, p := range indexPrefixes {
		if strings.HasPrefix(q.Code, p) {
			return 0
		}
	}
	return q.Amount / q.Volume
}

// inCallAuction 判断 HH:MM:SS 是否落在早盘集合竞价区间 [09:15, 09:25)。
func inCallAuction(t string) bool {
	return len(t) >= 5 && t[0:5] >= "09:15" && t[0:5] < "09:25"
}

// ParseFrame 解析一帧推送，可能含多行 `code=csv`。
// 也吃 REST 的 `var hq_str_code="csv";` 形式。
func ParseFrame(frame string) []Quote {
	var out []Quote
	for _, line := range strings.Split(frame, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "var hq_str_")
		line = strings.TrimSuffix(line, ";")
		code, csv, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if q, ok := Parse(code, strings.Trim(csv, `"`)); ok {
			out = append(out, q)
		}
	}
	return out
}
