package main

// TRACK D · Feature D2 — Find riders near a restaurant.        Redis type: GEO (a sorted set underneath)
//
// Problem: an order is ready at Koramangala. Which online riders are within 5 km, nearest first?
// Riders send their GPS every few seconds — thousands of writes per second. "Distance from every
// rider" in SQL is a full scan. Redis GEO indexes positions (geohash) and answers radius queries fast.
//
// Keys you own:   riders:geo              GEO      member = riderId, position = lng/lat
//                 rider:{riderId}:alive   STRING   heartbeat, expires in 30 s (no GPS for 30 s = offline)
//
//   POST /riders/{id}/location {"lat":12.93,"lng":77.62}     GET /riders/nearby?lat=..&lng=..&km=5
//
// Try it:  redis-cli GEOPOS riders:geo rider-ravi      redis-cli GEODIST riders:geo rider-ravi rider-imran km

const (
	RidersGeoKey          = "riders:geo"
	RiderAliveTTLSeconds  = 30
)

// NearbyRider represents a rider ID and distance in kilometers.
type NearbyRider struct {
	RiderID    string  `json:"riderId"`
	DistanceKm float64 `json:"distanceKm"`
}

func RiderAliveKey(riderID string) string {
	return "rider:" + riderID + ":alive"
}

// TODO(1): A rider's phone sent its location.
//   GEOADD riders:geo  lng lat  riderId        <-- LONGITUDE FIRST. The #1 GEO bug in real code.
//   SET rider:{id}:alive 1 EX RiderAliveTTLSeconds     (heartbeat)
func UpdateLocation(riderID string, lat, lng float64) error {
	panic("TODO(1) not implemented yet")
}

// TODO(2): Riders within radiusKm of (lat, lng), nearest first, at most limit.
//   GEOSEARCH riders:geo FROMLONLAT lng lat BYRADIUS radiusKm km ASC WITHDIST
//     -> [[riderId, "1.2345"], [riderId, "4.5678"], ...]   (distance as a string, in km)
//   Skip riders whose alive key is gone (EXISTS == 0) — and ZREM them from riders:geo so the
//   index cleans itself up lazily ("their phone died in a lift 10 minutes ago").
func Nearby(lat, lng, radiusKm float64, limit int) ([]NearbyRider, error) {
	panic("TODO(2) not implemented yet")
}

// TODO(3): The rider logged off. Remove them from riders:geo (it's a sorted set: ZREM) and delete the alive key.
func GoOffline(riderID string) error {
	panic("TODO(3) not implemented yet")
}
