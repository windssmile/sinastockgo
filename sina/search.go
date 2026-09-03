package sina

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// 注意：查询串要用 UTF-8 编码，响应却是 GBK。这个不对称没有文档，
// 用 GBK 发查询不会报错，只会静默返回一串热门股——排查起来很费时间。
const suggestURL = "https://suggest3.sinajs.cn/suggest/type=&key=%s"

// Hit 是一条搜索结果。Code 为空表示 sinajs 不提供该品种的行情。
type Hit struct {
	Name string
	Code string // sinajs 代码，如 sh600519 / rt_hk00700 / gb_aapl
	Kind Kind
	Why  string // Code 为空时说明原因
}

// toCode 把 suggest 的 (type, 代码, 完整代码) 三元组转成 sinajs 代码。
// type 取值由实测得到，不在表内的一律判为不支持，而不是拼一个大概率
// 拉不到数据的代码——静默多出一行空白比明说不支持更难查。
func toCode(typ, short, full string) (string, Kind, string) {
	switch typ {
	case "11", "12", "81", "203": // A股、B股、可转债、场内 ETF/LOF：full 已经是 sh/sz/bj 全码
		return full, KindCN, ""
	case "21", "22", "23", "201": // 场外及分级基金：full 已经是 ofXXXXXX
		return full, KindFund, ""
	case "103": // 伦交所。注意不能退回 gb_，gb_apgn 是另一家同名公司
		return "lse_" + short, KindUK, ""
	case "31": // 港股
		return "rt_hk" + short, KindHK, ""
	case "33": // 港股指数（hsi、hstech…）；A股指数走 11
		return "rt_hk" + strings.ToUpper(short), KindHK, ""
	case "41": // 美股与美股指数：.ixic → gb_$ixic
		return "gb_" + strings.Replace(short, ".", "$", 1), KindUS, ""
	case "71": // 外汇与数字币同用 71，靠代码前缀分流
		if strings.HasPrefix(short, "btc") {
			return "btc_" + short, KindCrypto, ""
		}
		return "fx_s" + short, KindFX, ""
	case "86": // 国外期货／现货
		return "hf_" + strings.ToUpper(short), KindHF, ""
	case "87", "88": // 国内期货（87 主力、88 连续）
		return "nf_" + strings.ToUpper(short), KindNF, ""
	case "26": // 公募 REITs。full 是 cf508000 这种自造前缀，hq 不认，得按代码段
		// 还原成交易所全码：508/5089 在沪，180 在深。行情布局与 A 股一致。
		switch {
		case strings.HasPrefix(short, "5"):
			return "sh" + short, KindCN, ""
		case strings.HasPrefix(short, "1"):
			return "sz" + short, KindCN, ""
		}
		return "", "", "无法判断交易所的 REITs 代码 " + short
	case "214": // 各国国债收益率，full 就是 us10yt / de10yt 这样的短码
		return "globalbd_" + short, KindBond, ""
	case "109":
		return "", "", "期权，新浪不提供行情"
	default:
		return "", "", "不支持的品种类型 " + typ
	}
}

// Verify 实拉一次确认该代码真有行情。
// suggest 列出的品种未必都有数据源——美元指数 diniw 就是搜得到、任何前缀都拉不到，
// 与其为每个边角情况维护黑名单，不如添加前问一次接口。
func Verify(ctx context.Context, code string) (Quote, error) {
	qs, err := Snapshot(ctx, []string{code})
	if err != nil {
		return Quote{}, err
	}
	if len(qs) == 0 {
		return Quote{}, fmt.Errorf("%s 无行情数据", code)
	}
	return qs[0], nil
}

// Search 按名称或代码搜索品种。
func Search(ctx context.Context, keyword string) ([]Hit, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf(suggestURL, url.QueryEscape(kw)), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", referer)
	req.Header.Set("User-Agent", ua)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := decodeGBK(resp.Body)
	if err != nil {
		return nil, err
	}
	hits := parseSuggest(body)
	if h, ok := csiIndexHit(ctx, kw); ok {
		hits = append(hits, h)
	}
	return hits, nil
}

// csiIndexHit 兜底中证指数（932000 中证2000 这类 9 开头六位码）。
// suggest 完全不收录它们，但 hq 用 si 前缀能拉到，布局与 A 股指数一致。
func csiIndexHit(ctx context.Context, kw string) (Hit, bool) {
	if len(kw) != 6 || !strings.HasPrefix(kw, "9") || !isNumeric(kw) {
		return Hit{}, false
	}
	q, err := Verify(ctx, "si"+kw)
	if err != nil || q.Name == "" {
		return Hit{}, false
	}
	return Hit{Name: q.Name, Code: "si" + kw, Kind: KindCN}, true
}

func parseSuggest(body string) []Hit {
	_, rest, ok := strings.Cut(body, `="`)
	if !ok {
		return nil
	}
	rest = strings.TrimSuffix(strings.TrimSpace(rest), `";`)

	var hits []Hit
	seen := map[string]bool{}
	for _, entry := range strings.Split(rest, ";") {
		f := strings.Split(entry, ",")
		if len(f) < 5 {
			continue
		}
		code, kind, why := toCode(f[1], f[2], f[3])
		if code != "" && seen[code] {
			continue
		}
		seen[code] = true
		hits = append(hits, Hit{Name: f[4], Code: code, Kind: kind, Why: why})
	}
	return hits
}
