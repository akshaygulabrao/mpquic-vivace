package congestion

import (
	"fmt"
	"time"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
	"math"
)

type VivaceSender struct {
	hybridSlowStart HybridSlowStart
	prr             PrrSender
	rttStats        *RTTStats
	stats           connectionStats
	history         [4]VivaceInterval
	vivaceSenders     map[protocol.PathID]*VivaceSender

	// Track the largest packet that has been sent.
	largestSentPacketNumber protocol.PacketNumber

	// Track the largest packet that has been acked.
	largestAckedPacketNumber protocol.PacketNumber

	// Track the largest packet number outstanding when a CWND cutbacks occurs.
	largestSentAtLastCutback protocol.PacketNumber

	// Congestion window in packets.
	congestionWindow protocol.PacketNumber

	// Slow start congestion window in packets, aka ssthresh.
	slowstartThreshold protocol.PacketNumber

	// Whether the last loss event caused us to exit slowstart.
	// Used for stats collection of slowstartPacketsLost
	lastCutbackExitedSlowstart bool

	// When true, texist slow start with large cutback of congestion window.
	slowStartLargeReduction bool

	// Minimum congestion window in packets.
	minCongestionWindow protocol.PacketNumber

	// Maximum number of outstanding packets for tcp.
	maxTCPCongestionWindow protocol.PacketNumber

	// Number of connections to simulate
	numConnections int

	// ACK counter for the Reno implementation
	congestionWindowCount protocol.ByteCount

	lostPackets	uint32

	initialCongestionWindow    protocol.PacketNumber
	initialMaxCongestionWindow protocol.PacketNumber
}

func NewVivaceSender(vivaceSenders map[protocol.PathID]*VivaceSender, rttStats *RTTStats, initialCongestionWindow, initialMaxCongestionWindow protocol.PacketNumber) SendAlgorithmWithDebugInfo {
	v := &VivaceSender{
		rttStats:                   rttStats,
		initialCongestionWindow:    initialCongestionWindow,
		initialMaxCongestionWindow: initialMaxCongestionWindow,
		congestionWindow:           initialCongestionWindow,
		minCongestionWindow:        defaultMinimumCongestionWindow,
		slowstartThreshold:         initialMaxCongestionWindow,
		maxTCPCongestionWindow:     initialMaxCongestionWindow,
		numConnections:             defaultNumConnections,
		vivaceSenders:                vivaceSenders,
		lostPackets:			0,
	}
	v.InitIntervals();
	return v
}
func(v *VivaceSender) printIntervals(){
	for i:=0; i < 4; i++ {
		a := v.history[i]
		fmt.Printf("Sent:%v,Acked:%v,Lost:%v  ", a.sentPackets, a.ackedPackets, a.lostPackets)
	}
	fmt.Printf("\n")
}
func(v *VivaceSender) utilityFunction() float64{
	ThroughputReward := math.Pow(v.history[1].rate,0.9) * float64(int64(protocol.ByteCount(protocol.DefaultTCPMSS)))
	var LatencyGradient float64
	LatencyGradient = float64(v.history[1].duration.Nanoseconds() - v.history[2].duration.Nanoseconds()) / 1e6
	LatencyPenalty := float64(900*v.history[1].rate * LatencyGradient)
	utils.Infof("%v",LatencyGradient)

	var LossRate float64
	if v.history[1].sentPackets == 0{
		LossRate = 0
	} else{
		LossRate = float64(v.history[1].lostPackets) / float64(v.history[1].sentPackets)
	}
	LossPenalty := v.history[1].rate * LossRate

	utils.Infof("Tp:%v Lat: %v Loss: %v", ThroughputReward,LatencyPenalty,LossPenalty)
	return ThroughputReward - LatencyPenalty - LossPenalty
}
func(v *VivaceSender) InitIntervals(){
	for i:= 0; i < 4; i++ {
		v.history[i] = NewVivaceInterval(v.rttStats.SmoothedRTT(),time.Time{})
	}
}

func (v *VivaceSender) TimeUntilSend(now time.Time, bytesInFlight protocol.ByteCount) time.Duration {
	if v.InRecovery() {
		// PRR is used when in recovery.
		return v.prr.TimeUntilSend(v.GetCongestionWindow(), bytesInFlight, v.GetSlowStartThreshold())
	}
	if v.GetCongestionWindow() > bytesInFlight {
		return 0
	}
	return utils.InfDuration
}

func (v *VivaceSender) OnPacketSent(sentTime time.Time, bytesInFlight protocol.ByteCount, packetNumber protocol.PacketNumber, bytes protocol.ByteCount, isRetransmittable bool) bool {
	v.history[0].sentPackets++
	// Only update bytesInFlight for data packets.

	if !isRetransmittable {
		return false
	}
	if v.InRecovery() {
		// PRR is used when in recovery.
		v.prr.OnPacketSent(bytes)
	}
	v.largestSentPacketNumber = packetNumber
	v.hybridSlowStart.OnPacketSent(packetNumber)
	return true
}

func (v *VivaceSender) GetCongestionWindow() protocol.ByteCount {
	start := v.history[0].start
	duration := v.history[0].duration
	if start.Add(duration).Before(time.Now()){
		for i:= 3; i > 0 ; i-- {
			v.history[i] = v.history[i-1]
		}
		v.history[0] = NewVivaceInterval( v.rttStats.SmoothedRTT(), time.Now())
	}
	newCwnd:= v.history[1].rate + v.utilityFunction()
	utils.Infof(" %v",newCwnd)
	//return protocol.ByteCount(uint64(v.history[1].rate)) * protocol.DefaultTCPMSS
	return protocol.ByteCount(int64(newCwnd ))
}

func (v *VivaceSender) GetSlowStartThreshold() protocol.ByteCount {
	return protocol.ByteCount(v.slowstartThreshold) * protocol.DefaultTCPMSS
}

func (v *VivaceSender) InRecovery() bool {
	return false
}

func (v *VivaceSender) BandwidthEstimate() Bandwidth{
	return BandwidthFromDelta(v.GetCongestionWindow(), v.rttStats.SmoothedRTT())
}
func (v *VivaceSender) HybridSlowStart() *HybridSlowStart {
	return &v.hybridSlowStart
}

func (v *VivaceSender) MaybeExitSlowStart() {
}

func (v *VivaceSender) OnConnectionMigration() {

}
func (v *VivaceSender)  OnPacketAcked(protocol.PacketNumber, protocol.ByteCount, protocol.ByteCount) {
	v.history[0].ackedPackets++
}

func (v *VivaceSender) OnPacketLost(protocol.PacketNumber, protocol.ByteCount, protocol.ByteCount) {
	v.history[0].lostPackets++
}

func (v *VivaceSender) OnRetransmissionTimeout(bool) {

}

func (v *VivaceSender) RenoBeta() float32{
	return 0.0
}

func (v *VivaceSender) RetransmissionDelay() time.Duration{
	return 1 * time.Millisecond
}

func (v *VivaceSender) SetNumEmulatedConnections(int){
}

func (v *VivaceSender) SetSlowStartLargeReduction(bool){

}

func (v *VivaceSender) SlowstartThreshold() protocol.PacketNumber {
	return 0
}

func (v *VivaceSender) SmoothedRTT() time.Duration {
	return v.rttStats.SmoothedRTT()
}
