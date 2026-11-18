package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/graphql-go/graphql"
)

const Port = ":8086"

// ---------- Domain Models (DB Tables) ----------

type Restaurant struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Cuisine         string  `json:"cuisine"`
	Rating          float64 `json:"rating"`
	DeliveryMinutes int     `json:"deliveryMinutes"`
}

type MenuItem struct {
	ID           string `json:"id"`
	RestaurantID string `json:"restaurantId"`
	Name         string `json:"name"`
	Price        int    `json:"price"`
	Veg          bool   `json:"veg"`
	Bestseller   bool   `json:"bestseller"`
}

type Review struct {
	ID           string  `json:"id"`
	RestaurantID string  `json:"restaurantId"`
	Author       string  `json:"author"`
	Rating       int     `json:"rating"`
	Comment      *string `json:"comment"`
}

var restaurants = []Restaurant{
	{ID: "r1", Name: "Dosa Junction", Cuisine: "South Indian", Rating: 4.5, DeliveryMinutes: 25},
	{ID: "r2", Name: "Biryani Brothers", Cuisine: "Hyderabadi", Rating: 4.3, DeliveryMinutes: 35},
	{ID: "r3", Name: "Punjab Da Dhaba", Cuisine: "North Indian", Rating: 4.1, DeliveryMinutes: 30},
	{ID: "r4", Name: "Idli Express", Cuisine: "South Indian", Rating: 4.6, DeliveryMinutes: 20},
	{ID: "r5", Name: "Wok This Way", Cuisine: "Chinese", Rating: 3.9, DeliveryMinutes: 40},
}

var menuItems = []MenuItem{
	{ID: "m1", RestaurantID: "r1", Name: "Masala Dosa", Price: 129, Veg: true, Bestseller: true},
	{ID: "m2", RestaurantID: "r1", Name: "Rava Dosa", Price: 149, Veg: true, Bestseller: false},
	{ID: "m3", RestaurantID: "r1", Name: "Filter Coffee", Price: 49, Veg: true, Bestseller: true},
	{ID: "m4", RestaurantID: "r2", Name: "Chicken Dum Biryani", Price: 299, Veg: false, Bestseller: true},
	{ID: "m5", RestaurantID: "r2", Name: "Veg Biryani", Price: 219, Veg: true, Bestseller: false},
	{ID: "m6", RestaurantID: "r2", Name: "Double Ka Meetha", Price: 99, Veg: true, Bestseller: false},
	{ID: "m7", RestaurantID: "r3", Name: "Butter Chicken", Price: 329, Veg: false, Bestseller: true},
	{ID: "m8", RestaurantID: "r3", Name: "Dal Makhani", Price: 229, Veg: true, Bestseller: true},
	{ID: "m9", RestaurantID: "r3", Name: "Garlic Naan", Price: 59, Veg: true, Bestseller: false},
	{ID: "m10", RestaurantID: "r4", Name: "Ghee Podi Idli", Price: 119, Veg: true, Bestseller: true},
	{ID: "m11", RestaurantID: "r4", Name: "Medu Vada", Price: 79, Veg: true, Bestseller: false},
	{ID: "m12", RestaurantID: "r5", Name: "Hakka Noodles", Price: 189, Veg: true, Bestseller: true},
	{ID: "m13", RestaurantID: "r5", Name: "Chilli Chicken", Price: 249, Veg: false, Bestseller: false},
}

var (
	reviewsMu   sync.RWMutex
	reviewsList = []Review{
		{ID: "v1", RestaurantID: "r1", Author: "Asha", Rating: 5, Comment: strPtr("Crispiest dosa in Koramangala")},
		{ID: "v2", RestaurantID: "r1", Author: "Rahul", Rating: 4, Comment: strPtr("Coffee could be stronger")},
		{ID: "v3", RestaurantID: "r2", Author: "Imran", Rating: 5, Comment: strPtr("Proper dum biryani")},
		{ID: "v4", RestaurantID: "r3", Author: "Simran", Rating: 4, Comment: nil},
		{ID: "v5", RestaurantID: "r4", Author: "Karthik", Rating: 5, Comment: strPtr("Podi idli is unreal")},
	}
	reviewSeq   atomic.Int64
	menuQueries atomic.Int64 // Counts menu queries to monitor N+1 behavior
)

func strPtr(s string) *string { return &s }

// ---------- GraphQL Schema Definition ----------

func buildSchema() (graphql.Schema, error) {
	reviewSeq.Store(5)

	menuItemType := graphql.NewObject(graphql.ObjectConfig{
		Name: "MenuItem",
		Fields: graphql.Fields{
			"id":         &graphql.Field{Type: graphql.NewNonNull(graphql.ID)},
			"name":       &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"price":      &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			"veg":        &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)},
			"bestseller": &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean)},
		},
	})

	reviewType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Review",
		Fields: graphql.Fields{
			"id":      &graphql.Field{Type: graphql.NewNonNull(graphql.ID)},
			"author":  &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"rating":  &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			"comment": &graphql.Field{Type: graphql.String},
		},
	})

	restaurantType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Restaurant",
		Fields: graphql.Fields{
			"id":              &graphql.Field{Type: graphql.NewNonNull(graphql.ID)},
			"name":            &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"cuisine":         &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"rating":          &graphql.Field{Type: graphql.NewNonNull(graphql.Float)},
			"deliveryMinutes": &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
			// Resolver for nested menu (runs only if requested by client!)
			"menu": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(menuItemType))),
				Args: graphql.FieldConfigArgument{
					"vegOnly": &graphql.ArgumentConfig{
						Type:         graphql.Boolean,
						DefaultValue: false,
					},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					r, ok := p.Source.(Restaurant)
					if !ok {
						return nil, nil
					}
					menuQueries.Add(1)
					vegOnly, _ := p.Args["vegOnly"].(bool)

					var result []MenuItem
					for _, item := range menuItems {
						if item.RestaurantID == r.ID {
							if !vegOnly || item.Veg {
								result = append(result, item)
							}
						}
					}
					return result, nil
				},
			},
			// Resolver for nested reviews (latest first)
			"reviews": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(reviewType))),
				Args: graphql.FieldConfigArgument{
					"last": &graphql.ArgumentConfig{
						Type:         graphql.Int,
						DefaultValue: 3,
					},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					r, ok := p.Source.(Restaurant)
					if !ok {
						return nil, nil
					}
					last, _ := p.Args["last"].(int)
					if last <= 0 {
						last = 3
					}

					reviewsMu.RLock()
					var mine []Review
					for _, rev := range reviewsList {
						if rev.RestaurantID == r.ID {
							mine = append(mine, rev)
						}
					}
					reviewsMu.RUnlock()

					// Reverse for newest first
					for i, j := 0, len(mine)-1; i < j; i, j = i+1, j-1 {
						mine[i], mine[j] = mine[j], mine[i]
					}
					if len(mine) > last {
						mine = mine[:last]
					}
					return mine, nil
				},
			},
		},
	})

	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			// Query.restaurants(cuisine: String)
			"restaurants": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(restaurantType))),
				Args: graphql.FieldConfigArgument{
					"cuisine": &graphql.ArgumentConfig{Type: graphql.String},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					cuisineArg, ok := p.Args["cuisine"].(string)
					if !ok || strings.TrimSpace(cuisineArg) == "" {
						return restaurants, nil
					}
					target := strings.ToLower(strings.TrimSpace(cuisineArg))
					var filtered []Restaurant
					for _, r := range restaurants {
						if strings.ToLower(r.Cuisine) == target {
							filtered = append(filtered, r)
						}
					}
					return filtered, nil
				},
			},
			// Query.restaurant(id: ID!)
			"restaurant": &graphql.Field{
				Type: restaurantType,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					id, _ := p.Args["id"].(string)
					for _, r := range restaurants {
						if r.ID == id {
							return r, nil
						}
					}
					return nil, nil
				},
			},
		},
	})

	reviewInputType := graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "ReviewInput",
		Fields: graphql.InputObjectConfigFieldMap{
			"restaurantId": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.ID)},
			"rating":       &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.Int)},
			"comment":      &graphql.InputObjectFieldConfig{Type: graphql.String},
		},
	})

	mutationType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			// Mutation.addReview(input: ReviewInput!)
			"addReview": &graphql.Field{
				Type: graphql.NewNonNull(reviewType),
				Args: graphql.FieldConfigArgument{
					"input": &graphql.ArgumentConfig{Type: graphql.NewNonNull(reviewInputType)},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					input, ok := p.Args["input"].(map[string]any)
					if !ok {
						return nil, fmt.Errorf("invalid input")
					}
					restaurantID, _ := input["restaurantId"].(string)
					found := false
					for _, r := range restaurants {
						if r.ID == restaurantID {
							found = true
							break
						}
					}
					if !found {
						return nil, fmt.Errorf("No restaurant %s", restaurantID)
					}

					rating, _ := input["rating"].(int)
					if rating < 1 || rating > 5 {
						return nil, fmt.Errorf("rating must be 1..5")
					}

					var comment *string
					if c, ok := input["comment"].(string); ok && c != "" {
						comment = &c
					}

					newID := fmt.Sprintf("v%d", reviewSeq.Add(1))
					rev := Review{
						ID:           newID,
						RestaurantID: restaurantID,
						Author:       "guest",
						Rating:       rating,
						Comment:      comment,
					}

					reviewsMu.Lock()
					reviewsList = append(reviewsList, rev)
					reviewsMu.Unlock()

					return rev, nil
				},
			},
		},
	})

	return graphql.NewSchema(graphql.SchemaConfig{
		Query:    queryType,
		Mutation: mutationType,
	})
}

// ---------- Handlers ----------

type Server struct {
	schema graphql.Schema
}

func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "POST a JSON body {query, variables}"})
		return
	}

	var reqBody struct {
		Query         string         `json:"query"`
		Variables     map[string]any `json:"variables"`
		OperationName string         `json:"operationName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "malformed JSON request"})
		return
	}

	menuQueries.Store(0)
	start := time.Now()

	params := graphql.Params{
		Schema:         s.schema,
		RequestString:  reqBody.Query,
		VariableValues: reqBody.Variables,
		OperationName:  reqBody.OperationName,
	}
	result := graphql.Do(params)

	elapsed := time.Since(start)
	log.Printf("[graphql] %v, menu table queried %d time(s), errors=%d",
		elapsed, menuQueries.Load(), len(result.Errors))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // GraphQL returns HTTP 200 even for query execution errors
	_ = json.NewEncoder(w).Encode(result)
}

// REST fallback for comparison
func (s *Server) handleRest(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/") // ["", "rest", "restaurants", id?, "menu"?]
	w.Header().Set("Content-Type", "application/json")

	if len(parts) == 3 {
		_ = json.NewEncoder(w).Encode(restaurants)
		return
	}
	if len(parts) == 5 && parts[4] == "menu" {
		id := parts[3]
		var filtered []MenuItem
		for _, m := range menuItems {
			if m.RestaurantID == id {
				filtered = append(filtered, m)
			}
		}
		_ = json.NewEncoder(w).Encode(filtered)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", s.handleGraphQL)
	mux.HandleFunc("GET /rest/restaurants", s.handleRest)
	mux.HandleFunc("GET /rest/restaurants/{id}/menu", s.handleRest)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, playgroundHTML)
	})
	return mux
}

func main() {
	schema, err := buildSchema()
	if err != nil {
		log.Fatalf("failed to build GraphQL schema: %v", err)
	}

	server := &Server{schema: schema}
	log.Printf("QuickBite GraphQL Server (Go) running on http://localhost%s (open playground in browser)", Port)
	if err := http.ListenAndServe(Port, server.Routes()); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

const playgroundHTML = `<!doctype html><html><head><meta charset="utf-8"><title>QuickBite GraphQL (Go)</title>
<style>
  body{font-family:system-ui,sans-serif;background:#0b0033;color:#fff;margin:0;padding:20px}
  h1{margin:0 0 12px;background:linear-gradient(90deg,#e535ab,#ffb300);-webkit-background-clip:text;color:transparent}
  .row{display:flex;gap:16px;flex-wrap:wrap}.col{flex:1;min-width:320px}
  textarea,pre{width:100%;box-sizing:border-box;background:#1d0b5a;color:#e0f7fa;border:2px solid #e535ab;border-radius:12px;padding:12px;font:13px ui-monospace,monospace}
  textarea{height:300px}#vars{height:70px}pre{min-height:390px;white-space:pre-wrap;border-color:#00e5ff}
  button{background:#e535ab;color:#fff;border:0;border-radius:10px;padding:8px 14px;font-weight:700;cursor:pointer;margin:0 6px 8px 0}
  .ex{background:#304ffe}
</style></head><body>
<h1>QuickBite GraphQL Playground (Go)</h1>
<button class="ex" data-q="home">Home screen</button><button class="ex" data-q="one">One restaurant</button>
<button class="ex" data-q="mut">Add review</button><button class="ex" data-q="intro">Introspection</button>
<div class="row"><div class="col">
  <textarea id="q"></textarea><textarea id="vars" placeholder='variables JSON, e.g. {"id":"r1"}'></textarea>
  <button id="run">Run ▶</button> <span id="meta"></span>
</div><div class="col"><pre id="out"></pre></div></div>
<script>
  const samples = {
    home: ` + "`" + `query Home {
  restaurants(cuisine: "South Indian") {
    name
    rating
    menu(vegOnly: true) { name price }
  }
}` + "`" + `,
    one: ` + "`" + `query One($id: ID!) {
  restaurant(id: $id) {
    name
    deliveryMinutes
    reviews(last: 2) { author rating comment }
  }
}` + "`" + `,
    mut: ` + "`" + `mutation Review {
  addReview(input: {restaurantId: "r1", rating: 5, comment: "Loved it"}) {
    id author rating
  }
}` + "`" + `,
    intro: ` + "`" + `query Introspect {
  __type(name: "Restaurant") {
    fields { name type { name kind } }
  }
}` + "`" + `
  };
  const $ = id => document.getElementById(id);
  document.querySelectorAll('.ex').forEach(b => b.onclick = () => { $('q').value = samples[b.dataset.q]; $('vars').value = b.dataset.q === 'one' ? '{"id":"r1"}' : ''; });
  $('q').value = samples.home;
  $('run').onclick = async () => {
    const t = performance.now();
    const r = await fetch('/graphql', {method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({query: $('q').value, variables: $('vars').value ? JSON.parse($('vars').value) : {}})});
    const text = await r.text();
    $('meta').textContent = ` + "`" + `HTTP ${r.status} · ${text.length} bytes · ${Math.round(performance.now() - t)} ms` + "`" + `;
    $('out').textContent = JSON.stringify(JSON.parse(text), null, 2);
  };
</script></body></html>`

