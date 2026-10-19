package main

import (
	"fmt"
	"testing"
)

func testProducts() []string {
	products := make([]string, 50)
	for i := range products {
		products[i] = fmt.Sprintf("product-%02d", i)
	}
	return products
}

func TestSkewedSelectorIsExactEightyTwenty(t *testing.T) {
	products := testProducts()
	selector := newCartSelector("skewed", products)
	hot, cold := 0, 0
	for i := 0; i < 100; i++ {
		product := selector.next()[0].ProductID
		if product >= products[0] && product <= products[4] {
			hot++
		} else {
			cold++
		}
	}
	if hot != 80 || cold != 20 {
		t.Fatalf("got hot/cold %d/%d, want 80/20", hot, cold)
	}
}

func TestMultiItemSelectorUsesDistinctHotProducts(t *testing.T) {
	products := testProducts()
	selector := newCartSelector("multi-item", products)
	ascending, nonAscending := 0, 0
	for i := 0; i < 100; i++ {
		cart := selector.next()
		if len(cart) != 2 && len(cart) != 3 {
			t.Fatalf("cart has %d lines", len(cart))
		}
		seen := map[string]bool{}
		for _, item := range cart {
			if item.ProductID < products[0] || item.ProductID > products[4] || seen[item.ProductID] {
				t.Fatalf("invalid hot cart: %#v", cart)
			}
			seen[item.ProductID] = true
		}
		ordered := true
		for line := 1; line < len(cart); line++ {
			if cart[line-1].ProductID > cart[line].ProductID {
				ordered = false
			}
		}
		if ordered {
			ascending++
		} else {
			nonAscending++
		}
	}
	if ascending == 0 || nonAscending == 0 {
		t.Fatalf("expected both ascending and opposite line orders, got %d/%d", ascending, nonAscending)
	}
}
