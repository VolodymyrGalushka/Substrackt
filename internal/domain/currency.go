package domain

import "strings"

// currencyScales holds the minor-unit digits for currencies that do not use 2.
var currencyScales = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0,
	"KRW": 0, "PYG": 0, "RWF": 0, "UGX": 0, "VND": 0, "VUV": 0, "XAF": 0,
	"XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
	"BTC": 8, "ETH": 8, "DOGE": 8, "LTC": 8,
	"USDC": 6, "USDT": 6,
	"SOL": 9,
}

// ScaleFor returns the number of decimal digits normally used by currency.
func ScaleFor(currency string) int {
	if scale, ok := currencyScales[strings.ToUpper(strings.TrimSpace(currency))]; ok {
		return scale
	}
	return 2
}
