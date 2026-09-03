package sina

import (
	"strings"
	"testing"
)

// 全部来自 2026-08-30 对 hq.sinajs.cn / w.sinajs.cn 的真实抓包。
const fixture = `var hq_str_sh000001="上证指数,3950.2368,3956.5710,3952.1790,3970.3095,3947.7981,0,0,510581645,970365152113,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,2026-08-28,15:43:32,00,";
var hq_str_sz300059="东方财富,19.550,19.620,19.420,19.630,19.420,19.410,19.420,180308294,3513704072.240,991400,19.410,1364200,19.400,214400,19.390,287200,19.380,155700,19.370,257234,19.420,684100,19.430,400700,19.440,475416,19.450,444405,19.460,2026-08-28,16:29:30,00,D|443100|8605002.000";
var hq_str_rt_hk00700="TENCENT,腾讯控股,444.000,447.800,462.200,443.400,455.200,7.400,1.653,455.200,455.400,12655334445.330,27742475,16.556,0.000,675.134,411.000,2026/08/28,16:08:36,100|0,N|Y|Y,455.400|432.800|478.000,0|||0.000|0.000|0.000, |0,Y";
var hq_str_gb_aapl="苹果,319.7000,1.63,2026-08-29 08:14:52,5.1200,316.8450,322.3700,315.4504,344.5700,225.1600,38649398,38515168,4665759510006,8.30,38.520000,0.00,0.00,0.00,0.00,14594180513,63,319.9000,0.06,0.20,Aug 28 07:59PM EDT,Aug 28 04:00PM EDT,314.5800,2782520,1,2026,12348919893.0000,320.2500,303.9100,889590318.6141,319.6400,314.5800";
var hq_str_hf_CL="83.467,,83.430,83.450,83.780,82.250,04:59:58,83.530,83.670,0,3,9,2026-08-29,纽约原油,0";
var hq_str_btc_btcbtcusd="08:36:19,0.0000,0.0000,78230.0000,0,78230.0000,78330.0000,78147.1700,78271.3300,比特币美元指数(BTC/USD),129.6500,2026-08-30,10140143.2786,20076918.0000,USD,";
var hq_str_nf_IF0="4591.800,4620.000,4587.200,4593.600,48813,224594285.400,140177.000,4593.600,0.000,5060.800,4140.800,0.000,0.000,4604.800,4600.800,142880.000,4593.000,1,0.000,0,0.000,0,0.000,0,0.000,0,4593.600,1,0.000,0,0.000,0,0.000,0,0.000,0,2026-08-28,15:00:00,300,1,,,,,,,,,4601.116,沪深300指数期货连续";
var hq_str_int_hangseng="恒生指数,25584.79,19.05,0.07";
var hq_str_gb_$ixic="纳斯达克,26402.4229,-0.52,2026-08-29 05:30:00,-138.9289,26515.9892,26700.6779,26359.2657,27190.2070,20690.2500,7526280685,6246241163,0,0.00,--,0.00,0.00,0.00,0.00,0,0,0.0000,0.00,0.00,,Aug 28 05:16PM EDT,26541.3518,0,1,2026,0.0000,0.0000,0.0000,0.0000,0.0000,0.0000";
var hq_str_fx_susdcny="02:56:19,6.7208000000,6.7491000000,6.7349000000,258.0000000000,6.7108000000,6.7349000000,6.7091000000,6.7349000000,在岸人民币,0.0000,0.0000,0.0258,此行情由新浪财经计算得出,0.0000,0.0000,,2026-08-29";
var hq_str_nf_RB0="螺纹钢连续,230000,3160.000,3180.000,3159.000,0.000,3177.000,3178.000,3178.000,0.000,3151.000,6,8,1152964.000,221502,沪,螺纹钢,2026-08-28,1,,,,,,,,,3172.461,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0";
var hq_str_nf_AU0="黄金连续,023000,997.000,1003.680,966.560,0.000,967.320,967.380,967.380,0.000,993.760,2,2,191637.000,426845,沪,黄金,2026-08-29,1,,,,,,,,,985.301,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0";
var hq_str_rt_hkHSI="HSI,恒生指数,25410.460,25565.740,25699.090,25410.460,25584.791,19.050,0.070,0.000,0.000,0.000,0,0.000,0.000,26783.240,17158.170,2026/08/28,16:08:36";
var hq_str_of510050="华夏上证50ETF,3.0342,4.5355,3.0414,-0.24,2026-08-28";
var hq_str_lse_hsba="hsba,1528.4,1531.2,1522,1515,1504,19865432,29177802265,2026-08-28 16:35:58,1528.4,29044,1529.2,30,120,2,15:35:27.588,,1";
var hq_str_lse_bp.="bp.,514.5,521.8,520.7,512.7,515.4,52678101,24744872053,2026-08-28 16:35:59,514.4,12,514.7,3941,120,2,15:35:31.052,,1";
var hq_str_sh118060="瑞可转债,197.600,199.342,193.107,202.716,193.000,193.039,193.168,117,0,0,193.039,0,0,0,0,0,0,0,0,0,193.168,0,0,0,0,0,0,0,0,2026-08-28,15:00:00,00,";
var hq_str_sh508000="张江REIT,1.988,1.989,1.992,1.992,1.965,1.991,1.992,196014,388531.000,300,1.991,42200,1.990,686,1.989,800,1.988,220800,1.986,3700,1.992,2200,1.993,100,1.996,220300,1.997,5300,1.998,2026-08-31,10:02:52,00,";
var hq_str_globalbd_us10yt="美国10年期国债,4.716,4.728,4.708,4.716,4.708,0,-0.4121,-0.0195,0,0,1788141080,2026-08-30,21:51:20,2036-08-15,4.625,1年2次,99.340,美国";
var hq_str_globalbd_de10yt="德国10年期国债,3.239,3.290,3.304,3.307,3.234,0,0.4255,0.0140,0,0,1787951171,2026-08-28,17:06:11,2036-08-15,3.000,1年1次,97.445,德国";
var hq_str_msci_STRD_USD_704844="MSCI CHINA ALL SHARES,MSCI中国全指,1740,1740,2174.67,0,1788328957168,1788328957168,1,0,06:02:37.000000000,0,0,9742,+,4,-17.7,-0.81,0,2188.45,2155.68,2188.42,2192.37,0,0,+-++--+-,,2026-09-01,,203422000000000,2192.37,2205.55,2189.94,2205.54,9741,9741,,0,,0,,0,0,2026-09-01,CNY,6,1";
var hq_str_hf_XAU="4454.23,4601.580,4454.23,4454.93,4631.18,4445.40,04:55:00,4601.58,4603.23,0,0,0,2026-08-29,伦敦金,0";`

func quotes(t *testing.T) map[string]Quote {
	t.Helper()
	m := map[string]Quote{}
	for _, q := range ParseFrame(fixture) {
		m[q.Code] = q
	}
	return m
}

func TestParseAllKinds(t *testing.T) {
	qs := quotes(t)
	want := []struct {
		code string
		kind Kind
		name string
		last float64
	}{
		{"sh000001", KindCN, "上证指数", 3952.1790},
		{"sz300059", KindCN, "东方财富", 19.420},
		{"rt_hk00700", KindHK, "腾讯控股", 455.200},
		{"gb_aapl", KindUS, "苹果", 319.70},
		{"gb_$ixic", KindUS, "纳斯达克", 26402.4229},
		{"hf_CL", KindHF, "纽约原油", 83.467},
		{"nf_IF0", KindNF, "沪深300指数期货连续", 4593.600},
		{"int_hangseng", KindIntIdx, "恒生指数", 25584.79},
		{"fx_susdcny", KindFX, "在岸人民币", 6.7349},
		{"btc_btcbtcusd", KindCrypto, "比特币美元指数(BTC/USD)", 78230.0},
		{"nf_RB0", KindNF, "螺纹钢连续", 3178.000},
		{"nf_AU0", KindNF, "黄金连续", 967.380},
		{"rt_hkHSI", KindHK, "恒生指数", 25584.791},
		{"of510050", KindFund, "华夏上证50ETF", 3.0342},
		{"hf_XAU", KindHF, "伦敦金", 4454.23},
		{"lse_hsba", KindUK, "hsba", 1528.4},
		{"globalbd_us10yt", KindBond, "美国10年期国债", 4.708},
		{"globalbd_de10yt", KindBond, "德国10年期国债", 3.304},
		{"lse_bp.", KindUK, "bp.", 514.5},
		{"sh118060", KindCN, "瑞可转债", 193.107},
		{"sh508000", KindCN, "张江REIT", 1.992},
		{"msci_STRD_USD_704844", KindMSCI, "MSCI中国全指", 2174.67},
	}
	if len(qs) != len(want) {
		t.Fatalf("解析出 %d 条，期望 %d", len(qs), len(want))
	}
	for _, w := range want {
		q, ok := qs[w.code]
		if !ok {
			t.Errorf("%s: 未解析出", w.code)
			continue
		}
		if q.Kind != w.kind || q.Name != w.name || q.Last != w.last {
			t.Errorf("%s: 得到 kind=%s name=%q last=%v，期望 %s / %q / %v",
				w.code, q.Kind, q.Name, q.Last, w.kind, w.name, w.last)
		}
		// 现价必须落在当日高低区间内——这是所有布局映射是否对位的总校验。
		if q.High > 0 && q.Low > 0 && (q.Last > q.High || q.Last < q.Low) {
			t.Errorf("%s: 现价 %v 不在 [%v, %v] 区间，字段错位", w.code, q.Last, q.Low, q.High)
		}
		// int_ 的报文里确实没有日期字段，其余品种必须有。
		if q.Date == "" && w.kind != KindIntIdx {
			t.Errorf("%s: 日期为空", w.code)
		}
	}
}

func TestLevel5OnlyForCN(t *testing.T) {
	qs := quotes(t)
	q := qs["sz300059"]
	if len(q.Bids) != 5 || len(q.Asks) != 5 {
		t.Fatalf("A股应有五档，得到 %d 买 / %d 卖", len(q.Bids), len(q.Asks))
	}
	if q.Bids[0].Price != 19.410 || q.Bids[0].Vol != 991400 {
		t.Errorf("买一 = %v @ %v，期望 991400 @ 19.410", q.Bids[0].Vol, q.Bids[0].Price)
	}
	if q.Asks[4].Price != 19.460 || q.Asks[4].Vol != 444405 {
		t.Errorf("卖五 = %v @ %v，期望 444405 @ 19.460", q.Asks[4].Vol, q.Asks[4].Price)
	}
	if len(qs["gb_aapl"].Bids) != 0 || len(qs["hf_CL"].Bids) != 0 {
		t.Error("非 A 股不应有五档")
	}
}

func TestChangeComputedOrRead(t *testing.T) {
	qs := quotes(t)
	// 港股/美股直接给涨跌额，应原样采用而非重算。
	if got := qs["rt_hk00700"].Change; got != 7.400 {
		t.Errorf("港股涨跌额 = %v，期望 7.400", got)
	}
	if got := qs["gb_aapl"].ChangePct; got != 1.63 {
		t.Errorf("美股涨跌幅 = %v，期望 1.63", got)
	}
	// A股不给，需由 现价-昨收 算出。
	if got := qs["sz300059"].Change; got < -0.2001 || got > -0.1999 {
		t.Errorf("A股涨跌额 = %v，期望 ≈ -0.200", got)
	}
	// nf_ 没有昨收，必须回退到昨结算 4604.800。
	if got := qs["nf_IF0"].Prev; got != 4604.800 {
		t.Errorf("国内期货基准价 = %v，期望昨结算 4604.800", got)
	}
	// 外汇：现价 6.7349 - 昨收 6.7091 恰为报文给出的涨跌额 0.0258，
	// 这个自洽关系是定位 fx_ 布局的唯一依据，改布局时必须仍然成立。
	fx := qs["fx_susdcny"]
	if d := fx.Last - fx.Prev - fx.Change; d > 1e-9 || d < -1e-9 {
		t.Errorf("外汇 现价%v-昨收%v 应等于涨跌额%v", fx.Last, fx.Prev, fx.Change)
	}
	// 数字币只给涨跌额不给昨收，需反推基准价，否则涨跌幅恒为 0。
	if btc := qs["btc_btcbtcusd"]; btc.Prev != 78230.0-129.65 || btc.ChangePct == 0 {
		t.Errorf("数字币 昨收=%v 涨跌幅=%v，期望反推出 78100.35 且涨跌幅非零", btc.Prev, btc.ChangePct)
	}
}

// nf_ 下两套布局同前缀不同构，选错了现价会读成开盘价而不报错。
func TestNFTwoLayouts(t *testing.T) {
	qs := quotes(t)
	fin, com := qs["nf_IF0"], qs["nf_RB0"]
	if fin.Open != 4591.800 || fin.Last != 4593.600 {
		t.Errorf("金融期货 开=%v 现=%v，期望 4591.800 / 4593.600", fin.Open, fin.Last)
	}
	if com.Open != 3160.000 || com.Last != 3178.000 || com.Prev != 3151.000 {
		t.Errorf("商品期货 开=%v 现=%v 昨结=%v，期望 3160/3178/3151", com.Open, com.Last, com.Prev)
	}
	// 商品期货的时间是 HHMMSS，须还原成可读格式。
	if com.Time != "23:00:00" || qs["nf_AU0"].Time != "02:30:00" {
		t.Errorf("时间未规整: RB0=%q AU0=%q", com.Time, qs["nf_AU0"].Time)
	}
}

// 英股的列序是 现价/最高/今开/最低/昨收，很反直觉。单看 hsba 时
// 「现价/最高/最低/今开/昨收」也说得通，是 bp. 把它证伪的（那样 最低 会高于 现价）。
// 所以这里必须两个样本一起断言，删掉任何一个都会让错误的列序蒙混过关。
func TestUKLayoutNeedsTwoSamples(t *testing.T) {
	qs := quotes(t)
	for _, c := range []string{"lse_hsba", "lse_bp."} {
		q, ok := qs[c]
		if !ok {
			t.Fatalf("%s 未解析出", c)
		}
		if q.Last < q.Low || q.Last > q.High {
			t.Errorf("%s: 现价 %v 不在 [%v, %v]，列序错了", c, q.Last, q.Low, q.High)
		}
		if q.Open < q.Low || q.Open > q.High {
			t.Errorf("%s: 今开 %v 不在 [%v, %v]，列序错了", c, q.Open, q.Low, q.High)
		}
	}
	if got := qs["lse_bp."]; got.Low != 512.7 || got.Open != 520.7 || got.Prev != 515.4 {
		t.Errorf("bp. 最低=%v 今开=%v 昨收=%v，期望 512.7 / 520.7 / 515.4",
			got.Low, got.Open, got.Prev)
	}
}

func TestEmptyAndMalformed(t *testing.T) {
	if _, ok := Parse("sh600000", ""); ok {
		t.Error("空 CSV 应判为无数据")
	}
	if got := ParseFrame("garbage without equals\n\n"); len(got) != 0 {
		t.Errorf("无效帧应解析出 0 条，得到 %d", len(got))
	}
	// WSS 推送格式（无 var 前缀、无引号）应与 REST 等价。
	ws := ParseFrame("sz300059=东方财富,19.550,19.620,19.420,19.630,19.420,19.410,19.420,180308294,3513704072.240")
	if len(ws) != 1 || ws[0].Last != 19.420 {
		t.Fatalf("WSS 帧解析失败: %+v", ws)
	}
	if len(ws[0].Bids) != 0 {
		t.Error("截断的行不应造出假盘口")
	}
}

func TestCallAuctionTopLevel(t *testing.T) {
	// 09:15–09:25 买卖一价为 0，应补成现价 10.50
	csv := `浦发银行,10.00,10.20,10.50,10.60,9.90,0.000,0.000,1000,10500,` +
		`100,0.000,200,10.10,300,10.05,400,10.00,500,9.95,` +
		`600,0.000,700,10.60,800,10.65,900,10.70,1000,10.75,` +
		`2026-08-31,09:18:00,00`
	q, ok := Parse("sh600000", csv)
	if !ok || len(q.Bids) == 0 {
		t.Fatal("parse failed")
	}
	if q.Bids[0].Price != 10.50 || q.Asks[0].Price != 10.50 {
		t.Fatalf("got bid1=%v ask1=%v, want 10.50", q.Bids[0].Price, q.Asks[0].Price)
	}
	// 连续竞价时段不动
	q2, _ := Parse("sh600000", strings.Replace(csv, "09:18:00", "10:18:00", 1))
	if q2.Bids[0].Price != 0 {
		t.Fatalf("continuous session should keep bid1=0, got %v", q2.Bids[0].Price)
	}
}

// 真实竞价报文：现价/最高/最低全是 0，虚拟匹配价只在竞买价里（2026-08-31 09:22 抓取）。
func TestCallAuctionLastFromBidPrice(t *testing.T) {
	csv := `浦发银行,0.000,9.000,0.000,0.000,0.000,9.000,9.000,0,0.000,` +
		`145200,9.000,52900,0.000,0,0.000,0,0.000,0,0.000,` +
		`145200,9.000,0,0.000,0,0.000,0,0.000,0,0.000,` +
		`2026-08-31,09:22:28,00,`
	q, ok := Parse("sh600000", csv)
	if !ok {
		t.Fatal("parse failed")
	}
	if q.Last != 9.0 || q.Bids[0].Price != 9.0 || q.Asks[0].Price != 9.0 {
		t.Fatalf("got last=%v bid1=%v ask1=%v, want 9", q.Last, q.Bids[0].Price, q.Asks[0].Price)
	}
	// 收盘/停牌时现价为 0 但不在竞价时段，不该借用竞买价
	q2, _ := Parse("sh600000", strings.Replace(csv, "09:22:28", "13:22:28", 1))
	if q2.Last != 0 {
		t.Fatalf("outside auction should keep last=0, got %v", q2.Last)
	}
}

// 国债的列序是靠涨跌额/幅反推的：昨收 = 涨跌额 / 涨跌幅，只有第 2 列对得上，
// 现价只能是第 3 列——单看美债三个数挨得太近分不出来，德债才把它钉死。
func TestBondYieldLayout(t *testing.T) {
	q := quotes(t)["globalbd_de10yt"]
	if q.Prev != 3.290 || q.Last != 3.304 || q.High != 3.307 || q.Low != 3.234 || q.Open != 3.239 {
		t.Fatalf("列序错位: open=%v prev=%v last=%v high=%v low=%v",
			q.Open, q.Prev, q.Last, q.High, q.Low)
	}
	if q.Change != 0.0140 || q.ChangePct != 0.4255 {
		t.Fatalf("涨跌应直接取报文给的值: %v %v", q.Change, q.ChangePct)
	}
	// 收益率 3.304% 对应的券价在「净价」里，别把两者搞混
	if got := fieldOf(q, "净价"); got != "97.445" {
		t.Fatalf("净价 = %q", got)
	}
	// 报文写的 2026-08-28 17:06:11 是纽约时间，入表要换成北京时间
	if q.Date != "2026-08-29" || q.Time != "05:06:11" {
		t.Fatalf("应换算成北京时间，得到 %q %q", q.Date, q.Time)
	}
	if fieldOf(q, "纽约时间") != "17:06:11" {
		t.Error("原始纽约时间应保留在详情字段里")
	}
	// 美债同理：纽约 08-30 21:51:20 = 北京 08-31 09:51:20
	if us := quotes(t)["globalbd_us10yt"]; us.Date != "2026-08-31" || us.Time != "09:51:20" {
		t.Fatalf("美债时间 = %q %q", us.Date, us.Time)
	}
}

func fieldOf(q Quote, k string) string {
	for _, f := range q.Fields {
		if f.K == k {
			return f.V
		}
	}
	return ""
}

func TestAvgPrice(t *testing.T) {
	cases := []struct {
		name string
		q    Quote
		want float64
	}{
		{"A股个股", Quote{Code: "sh600519", Kind: KindCN, Volume: 1000, Amount: 1_500_000}, 1500},
		{"港股", Quote{Code: "rt_hk00700", Kind: KindHK, Volume: 200, Amount: 100_000}, 500},
		{"A股指数不算", Quote{Code: "sh000001", Kind: KindCN, Volume: 1e9, Amount: 1e12}, 0},
		{"期货不算", Quote{Code: "nf_RB0", Kind: KindNF, Volume: 100, Amount: 300_000}, 0},
		{"无成交", Quote{Code: "sh600519", Kind: KindCN}, 0},
	}
	for _, c := range cases {
		if got := c.q.AvgPrice(); got != c.want {
			t.Errorf("%s: 均价 = %v, 期望 %v", c.name, got, c.want)
		}
	}
}
