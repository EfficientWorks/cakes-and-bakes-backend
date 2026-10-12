// Package geo holds small, dependency-free geography helpers: distance
// calculation and a pluggable geocoder (ZIP/address -> lat/lng).
package geo

import "math"

const earthRadiusMiles = 3958.8

// DistanceMiles returns the great-circle distance between two points.
func DistanceMiles(lat1, lng1, lat2, lng2 float64) float64 {
	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }

	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMiles * c
}
