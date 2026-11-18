package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGraphQLAPI(t *testing.T) {
	schema, err := buildSchema()
	if err != nil {
		t.Fatalf("failed to build schema: %v", err)
	}

	server := &Server{schema: schema}
	handler := server.Routes()

	// 1. Query restaurants with cuisine filter and nested menu
	t.Run("Query.restaurants with cuisine filter", func(t *testing.T) {
		query := `{
			restaurants(cuisine: "South Indian") {
				id
				name
				cuisine
				menu(vegOnly: true) {
					name
					price
					veg
				}
			}
		}`
		body, _ := json.Marshal(map[string]any{"query": query})
		req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp struct {
			Data struct {
				Restaurants []struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					Cuisine string `json:"cuisine"`
					Menu    []struct {
						Name  string `json:"name"`
						Price int    `json:"price"`
						Veg   bool   `json:"veg"`
					} `json:"menu"`
				} `json:"restaurants"`
			} `json:"data"`
			Errors []any `json:"errors"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if len(resp.Errors) > 0 {
			t.Fatalf("unexpected GraphQL errors: %v", resp.Errors)
		}
		if len(resp.Data.Restaurants) != 2 {
			t.Fatalf("expected 2 South Indian restaurants, got %d", len(resp.Data.Restaurants))
		}
		for _, r := range resp.Data.Restaurants {
			if r.Cuisine != "South Indian" {
				t.Errorf("expected South Indian cuisine, got %s", r.Cuisine)
			}
			for _, m := range r.Menu {
				if !m.Veg {
					t.Errorf("expected veg item only, got %s", m.Name)
				}
			}
		}
	})

	// 2. Query single restaurant by ID
	t.Run("Query.restaurant by ID", func(t *testing.T) {
		query := `{
			restaurant(id: "r1") {
				id
				name
				rating
			}
		}`
		body, _ := json.Marshal(map[string]any{"query": query})
		req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var resp struct {
			Data struct {
				Restaurant *struct {
					ID     string  `json:"id"`
					Name   string  `json:"name"`
					Rating float64 `json:"rating"`
				} `json:"restaurant"`
			} `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Data.Restaurant == nil || resp.Data.Restaurant.Name != "Dosa Junction" {
			t.Fatalf("expected Dosa Junction, got %+v", resp.Data.Restaurant)
		}
	})

	// 3. Mutation.addReview
	t.Run("Mutation.addReview", func(t *testing.T) {
		query := `mutation {
			addReview(input: {restaurantId: "r1", rating: 5, comment: "Delicious!"}) {
				id
				author
				rating
				comment
			}
		}`
		body, _ := json.Marshal(map[string]any{"query": query})
		req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var resp struct {
			Data struct {
				AddReview struct {
					ID     string `json:"id"`
					Author string `json:"author"`
					Rating int    `json:"rating"`
				} `json:"addReview"`
			} `json:"data"`
			Errors []any `json:"errors"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Errors) > 0 {
			t.Fatalf("mutation errors: %v", resp.Errors)
		}
		if resp.Data.AddReview.Rating != 5 || resp.Data.AddReview.Author != "guest" {
			t.Fatalf("unexpected review data: %+v", resp.Data.AddReview)
		}

		// Validation: rating > 5 -> returns errors in body
		badQuery := `mutation {
			addReview(input: {restaurantId: "r1", rating: 6}) { id }
		}`
		badBody, _ := json.Marshal(map[string]any{"query": badQuery})
		badReq := httptest.NewRequest("POST", "/graphql", bytes.NewReader(badBody))
		badRec := httptest.NewRecorder()
		handler.ServeHTTP(badRec, badReq)

		var badResp struct {
			Errors []any `json:"errors"`
		}
		_ = json.Unmarshal(badRec.Body.Bytes(), &badResp)
		if len(badResp.Errors) == 0 {
			t.Fatalf("expected GraphQL error for rating > 5")
		}
	})
}

