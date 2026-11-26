package catalog_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/craft/catalog"
)

func TestShopColorsMatchTheColourNumber(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	syncer := catalog.NewSyncer(st, quietLogger())
	source := &fakeSource{name: "preciosa", entries: []catalog.Entry{
		bead("311-19001-10_0-10010", "Matched", "red"),
		bead("311-19001-8_0-10010", "Matched in another size", "red"),
		bead("311-19001-10_0-99999", "Unknown number", "red"),
	}}
	if _, err := syncer.Run(ctx, source); err != nil {
		t.Fatalf("sync: %v", err)
	}

	page := `<table><tr><td>PRECIOSA</td><td>код RGB</td></tr><tr><td>10010</td><td>A1B2C3</td></tr></table>`
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, page) }))
	defer site.Close()
	shop := catalog.NewBiserShop(nil)
	shop.URL, shop.MinColors = site.URL, 1

	res, err := syncer.RunShopColors(ctx, shop)
	if err != nil || res.Updated != 2 {
		t.Fatalf("apply = %+v %v, want two updates", res, err)
	}
	item := itemByArticle(t, st, "311-19001-8_0-10010")
	if item.ShopColorHex != "#A1B2C3" || item.ColorHex != "" {
		t.Errorf("shop %q published %q", item.ShopColorHex, item.ColorHex)
	}
	if res, err = syncer.RunShopColors(ctx, shop); err != nil || res.Updated != 0 || res.Unchanged != 2 {
		t.Errorf("second apply = %+v %v, want no updates", res, err)
	}

	// A page without the table must not erase what is stored.
	page = "<p>maintenance</p>"
	if _, err := syncer.RunShopColors(ctx, shop); err == nil {
		t.Error("an empty page must fail the run")
	}
	if got := itemByArticle(t, st, "311-19001-10_0-10010").ShopColorHex; got != "#A1B2C3" {
		t.Errorf("colour after a failed run = %q", got)
	}
}
