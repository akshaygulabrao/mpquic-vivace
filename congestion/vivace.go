
package congestion

import (
	"time"
)


// VivaceInterval implements the vivace algorithm from MPTCP
type VivaceInterval struct {
	start	time.Time
	duration time.Duration
	sendingRate uint32
	lostPackets uint32
	ackedPackets uint32
}
//this should not be a pointer because we neeed to do a right shift in the array.
func NewVivaceInterval (duration time.Duration,currentTime time.Time) VivaceInterval {
	v := VivaceInterval{
		start: currentTime,
		duration: duration,
		sendingRate: 0,
		lostPackets: 0,
		ackedPackets: 0,
		}
	return v
}