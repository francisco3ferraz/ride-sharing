// Package fare calculates ride fares. It knows nothing about gRPC or HTTP.
package fare

import "time"

const (
	baseFareCents    = 150
	perKmCents       = 80
	perMinuteCents   = 20
	minimumFareCents = 400

	metersPerKm = 1000
	msPerMinute = 60_000
)

// Estimate returns the fare in cents for a trip of the given distance and duration.
//
// The distance and time charges are both fractions of a cent until the end. They are
// added over a common denominator (msPerMinute) and rounded once, half up, so rounding
// errors can't accumulate.
func Estimate(distanceMeters int32, d time.Duration) int64 {
	distanceCharge := perKmCents * int64(distanceMeters) * (msPerMinute / metersPerKm)
	timeCharge := perMinuteCents * d.Milliseconds()

	fare := baseFareCents + roundHalfUp(distanceCharge+timeCharge, msPerMinute)
	return max(fare, minimumFareCents)
}

// roundHalfUp divides n by d and rounds to the nearest integer, with halves going up.
// Both n and d must be non-negative.
func roundHalfUp(n, d int64) int64 {
	return (n + d/2) / d
}
