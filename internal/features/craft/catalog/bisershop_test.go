package catalog

import (
	"maps"
	"testing"
)

// The markup mimics the real page; the values are made up.
const shopPage = `<table><tr><td>00001</td><td>#123456</td></tr></table>
<table>
<thead><tr><td>PRECIOSA</td><td>DMC</td><td>код RGB </td><td>TOHO</td></tr></thead>
<tr><td>10010</td><td>111111</td><td>a1b2c3</td><td>222222</td></tr>
<tr><td><b>10010m</b></td><td></td><td>#0F0F0F</td><td></td></tr>
<tr><td>10010</td><td></td><td>FFFFFF</td><td></td></tr>
<tr><td>10020</td><td>333333</td><td>&nbsp;</td><td></td></tr>
<tr><td>10030</td><td></td><td>нет</td><td></td></tr>
<tr><td>всего</td><td></td><td>000000</td><td></td></tr>
</table>`

func TestParseShopColorsReadsOnlyTheRGBColumn(t *testing.T) {
	want := map[string]string{"10010": "#A1B2C3", "10010m": "#0F0F0F"}
	if got := parseShopColors([]byte(shopPage)); !maps.Equal(got, want) {
		t.Errorf("parseShopColors = %v, want %v", got, want)
	}
}
