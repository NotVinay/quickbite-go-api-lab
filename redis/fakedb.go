package main

import (
	"sync"
	"sync/atomic"
	"time"
)

// GIVEN — pretend this is QuickBite's Postgres database.
// Every menu read takes 300 ms (a slow join across 4 tables on a busy primary).
// Redis exists in this project to stop us paying that 300 ms again and again.

type Restaurant struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Cuisine string `json:"cuisine"`
}

type MenuItem struct {
	ItemID string `json:"itemId"`
	Name   string `json:"name"`
	Price  int    `json:"price"`
	Veg    bool   `json:"veg"`
}

const FakeDbQueryMs = 300 * time.Millisecond

// FakeDbMenuReads counts how many slow queries have hit the "database". The checker watches this.
var FakeDbMenuReads int64

var (
	fakeDbMu          sync.RWMutex
	fakeDbRestaurants = map[string]Restaurant{
		"r1": {ID: "r1", Name: "Dosa Junction", Cuisine: "South Indian"},
		"r2": {ID: "r2", Name: "Biryani Brothers", Cuisine: "Hyderabadi"},
		"r3": {ID: "r3", Name: "Punjab Da Dhaba", Cuisine: "North Indian"},
		"r4": {ID: "r4", Name: "Idli Express", Cuisine: "South Indian"},
		"r5": {ID: "r5", Name: "Wok This Way", Cuisine: "Chinese"},
	}

	fakeDbMenus = map[string][]MenuItem{
		"r1": {
			{ItemID: "masala-dosa", Name: "Masala Dosa", Price: 129, Veg: true},
			{ItemID: "rava-dosa", Name: "Rava Dosa", Price: 149, Veg: true},
			{ItemID: "filter-coffee", Name: "Filter Coffee", Price: 49, Veg: true},
		},
		"r2": {
			{ItemID: "chicken-biryani", Name: "Chicken Dum Biryani", Price: 299, Veg: false},
			{ItemID: "veg-biryani", Name: "Veg Biryani", Price: 219, Veg: true},
			{ItemID: "double-ka-meetha", Name: "Double Ka Meetha", Price: 99, Veg: true},
		},
		"r3": {
			{ItemID: "butter-chicken", Name: "Butter Chicken", Price: 329, Veg: false},
			{ItemID: "dal-makhani", Name: "Dal Makhani", Price: 229, Veg: true},
			{ItemID: "garlic-naan", Name: "Garlic Naan", Price: 59, Veg: true},
		},
		"r4": {
			{ItemID: "podi-idli", Name: "Ghee Podi Idli", Price: 119, Veg: true},
			{ItemID: "medu-vada", Name: "Medu Vada", Price: 79, Veg: true},
		},
		"r5": {
			{ItemID: "hakka-noodles", Name: "Hakka Noodles", Price: 189, Veg: true},
			{ItemID: "chilli-chicken", Name: "Chilli Chicken", Price: 249, Veg: false},
		},
	}

	RestaurantLocations = map[string][2]float64{
		"r1": {12.9340, 77.6260}, // Koramangala
		"r2": {12.9710, 77.6400}, // Indiranagar
		"r3": {12.9130, 77.6430}, // HSR Layout
		"r4": {12.9300, 77.6200}, // Koramangala 5th block
		"r5": {12.9690, 77.7490}, // Whitefield
	}

	RiderStarts = map[string][2]float64{
		"rider-ravi":    {12.9352, 77.6245}, // Koramangala
		"rider-imran":   {12.9719, 77.6412}, // Indiranagar
		"rider-asha":    {12.9121, 77.6446}, // HSR Layout
		"rider-karthik": {12.9698, 77.7500}, // Whitefield
		"rider-simran":  {12.8452, 77.6602}, // Electronic City
	}
)

// FakeDbGetMenu is SLOW. Returns nil if the restaurant doesn't exist.
func FakeDbGetMenu(restaurantID string) []MenuItem {
	atomic.AddInt64(&FakeDbMenuReads, 1)
	time.Sleep(FakeDbQueryMs)

	fakeDbMu.RLock()
	defer fakeDbMu.RUnlock()

	items, ok := fakeDbMenus[restaurantID]
	if !ok {
		return nil
	}
	res := make([]MenuItem, len(items))
	copy(res, items)
	return res
}

// FakeDbUpdatePrice updates the price of an item. Returns false if restaurant or item does not exist.
func FakeDbUpdatePrice(restaurantID, itemID string, newPrice int) bool {
	fakeDbMu.Lock()
	defer fakeDbMu.Unlock()

	items, ok := fakeDbMenus[restaurantID]
	if !ok {
		return false
	}

	found := false
	updated := make([]MenuItem, len(items))
	for i, it := range items {
		if it.ItemID == itemID {
			updated[i] = MenuItem{ItemID: it.ItemID, Name: it.Name, Price: newPrice, Veg: it.Veg}
			found = true
		} else {
			updated[i] = it
		}
	}
	if found {
		fakeDbMenus[restaurantID] = updated
	}
	return found
}

// FakeDbPriceOf provides fast price lookup for the cart. Returns (price, true) or (0, false).
func FakeDbPriceOf(itemID string) (int, bool) {
	fakeDbMu.RLock()
	defer fakeDbMu.RUnlock()

	for _, menu := range fakeDbMenus {
		for _, it := range menu {
			if it.ItemID == itemID {
				return it.Price, true
			}
		}
	}
	return 0, false
}

// FakeDbRestaurantOf tells which restaurant sells this item. Returns "" if not found.
func FakeDbRestaurantOf(itemID string) string {
	fakeDbMu.RLock()
	defer fakeDbMu.RUnlock()

	for rID, menu := range fakeDbMenus {
		for _, it := range menu {
			if it.ItemID == itemID {
				return rID
			}
		}
	}
	return ""
}

// FakeDbRestaurantName returns the restaurant name, or restaurantID if not found.
func FakeDbRestaurantName(restaurantID string) string {
	fakeDbMu.RLock()
	defer fakeDbMu.RUnlock()

	if r, ok := fakeDbRestaurants[restaurantID]; ok {
		return r.Name
	}
	return restaurantID
}
