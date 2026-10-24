package main

import "sync/atomic"

type cartSelector struct {
	mode     string
	products []string
	sequence atomic.Uint64
}

// newCartSelector creates a selector that picks carts from products using the given mode.
func newCartSelector(mode string, products []string) *cartSelector {
	return &cartSelector{mode: mode, products: products}
}

// next returns the cart for the next workflow: a single hot product, a skewed mix, two or three items, or products in turn.
func (s *cartSelector) next() []cartItem {
	sequence := s.sequence.Add(1) - 1
	switch s.mode {
	case "hot":
		return []cartItem{{ProductID: s.products[0], Quantity: 1}}
	case "skewed":
		if sequence%5 != 4 {
			return []cartItem{{ProductID: s.products[sequence%5], Quantity: 1}}
		}
		return []cartItem{{ProductID: s.products[5+(sequence/5)%45], Quantity: 1}}
	case "multi-item":
		size := 2 + int(sequence%2)
		start := int((sequence / 2) % 5)
		items := make([]cartItem, size)
		for i := range items {
			items[i] = cartItem{ProductID: s.products[(start+i)%5], Quantity: 1}
		}
		shuffle(items, sequence+1)
		return items
	default:
		return []cartItem{{ProductID: s.products[sequence%uint64(len(s.products))], Quantity: 1}}
	}
}

// shuffle reorders items in a way that depends only on seed, so runs can be repeated.
func shuffle(items []cartItem, seed uint64) {
	for i := len(items) - 1; i > 0; i-- {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		j := int(seed % uint64(i+1))
		items[i], items[j] = items[j], items[i]
	}
}
