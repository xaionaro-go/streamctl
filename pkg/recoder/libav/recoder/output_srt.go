package recoder

import (
	"fmt"
	"unsafe"

	"github.com/xaionaro-go/unsafetools"
)

// #cgo pkg-config: srt
// #include <srt/srt.h>
import "C"

type SRTStats struct {
	MsTimeStamp             int64
	PktSentTotal            int64
	PktRecvTotal            int64
	PktSndLossTotal         int
	PktRcvLossTotal         int
	PktRetransTotal         int
	PktSentACKTotal         int
	PktRecvACKTotal         int
	PktSentNAKTotal         int
	PktRecvNAKTotal         int
	UsSndDurationTotal      int64
	PktSndDropTotal         int
	PktRcvDropTotal         int
	PktRcvUndecryptTotal    int
	ByteSentTotal           uint64
	ByteRecvTotal           uint64
	ByteRcvLossTotal        uint64
	ByteRetransTotal        uint64
	ByteSndDropTotal        uint64
	ByteRcvDropTotal        uint64
	ByteRcvUndecryptTotal   uint64
	PktSent                 int64
	PktRecv                 int64
	PktSndLoss              int
	PktRcvLoss              int
	PktRetrans              int
	PktRcvRetrans           int
	PktSentACK              int
	PktRecvACK              int
	PktSentNAK              int
	PktRecvNAK              int
	MbpsSendRate            float64
	MbpsRecvRate            float64
	UsSndDuration           int64
	PktReorderDistance      int
	PktRcvAvgBelatedTime    float64
	PktRcvBelated           int64
	PktSndDrop              int
	PktRcvDrop              int
	PktRcvUndecrypt         int
	ByteSent                uint64
	ByteRecv                uint64
	ByteRcvLoss             uint64
	ByteRetrans             uint64
	ByteSndDrop             uint64
	ByteRcvDrop             uint64
	ByteRcvUndecrypt        uint64
	UsPktSndPeriod          float64
	PktFlowWindow           int
	PktCongestionWindow     int
	PktFlightSize           int
	MsRTT                   float64
	MbpsBandwidth           float64
	ByteAvailSndBuf         int
	ByteAvailRcvBuf         int
	MbpsMaxBW               float64
	ByteMSS                 int
	PktSndBuf               int
	ByteSndBuf              int
	MsSndBuf                int
	MsSndTsbPdDelay         int
	PktRcvBuf               int
	ByteRcvBuf              int
	MsRcvBuf                int
	MsRcvTsbPdDelay         int
	PktSndFilterExtraTotal  int
	PktRcvFilterExtraTotal  int
	PktRcvFilterSupplyTotal int
	PktRcvFilterLossTotal   int
	PktSndFilterExtra       int
	PktRcvFilterExtra       int
	PktRcvFilterSupply      int
	PktRcvFilterLoss        int
	PktReorderTolerance     int
	PktSentUniqueTotal      int64
	PktRecvUniqueTotal      int64
	ByteSentUniqueTotal     uint64
	ByteRecvUniqueTotal     uint64
	PktSentUnique           int64
	PktRecvUnique           int64
	ByteSentUnique          uint64
	ByteRecvUnique          uint64
}

func (output *Output) GetSRTStats() (*SRTStats, error) {
	privData := output.FormatContext.PrivateData()
	if privData == nil {
		return nil, fmt.Errorf("failed to get 'priv_data'")
	}

	privDataCP := *unsafetools.FieldByName(privData, "c").(*unsafe.Pointer)
	privDataC := C.int(uintptr(privDataCP))

	var stats C.SRT_TRACEBSTATS
	//size := C.sizeof_SRT_TRACEBSTATS
	if ret := C.srt_bistats(privDataC, &stats, 0, 1); ret != 0 {
		return nil, fmt.Errorf("srt_bistats() failed: %d", ret)
	}

	return &SRTStats{
		MsTimeStamp:             int64(stats.msTimeStamp),
		PktSentTotal:            int64(stats.pktSentTotal),
		PktRecvTotal:            int64(stats.pktRecvTotal),
		PktSndLossTotal:         int(stats.pktSndLossTotal),
		PktRcvLossTotal:         int(stats.pktRcvLossTotal),
		PktRetransTotal:         int(stats.pktRetransTotal),
		PktSentACKTotal:         int(stats.pktSentACKTotal),
		PktRecvACKTotal:         int(stats.pktRecvACKTotal),
		PktSentNAKTotal:         int(stats.pktSentNAKTotal),
		PktRecvNAKTotal:         int(stats.pktRecvNAKTotal),
		UsSndDurationTotal:      int64(stats.usSndDurationTotal),
		PktSndDropTotal:         int(stats.pktSndDropTotal),
		PktRcvDropTotal:         int(stats.pktRcvDropTotal),
		PktRcvUndecryptTotal:    int(stats.pktRcvUndecryptTotal),
		ByteSentTotal:           uint64(stats.byteSentTotal),
		ByteRecvTotal:           uint64(stats.byteRecvTotal),
		ByteRcvLossTotal:        uint64(stats.byteRcvLossTotal),
		ByteRetransTotal:        uint64(stats.byteRetransTotal),
		ByteSndDropTotal:        uint64(stats.byteSndDropTotal),
		ByteRcvDropTotal:        uint64(stats.byteRcvDropTotal),
		ByteRcvUndecryptTotal:   uint64(stats.byteRcvUndecryptTotal),
		PktSent:                 int64(stats.pktSent),
		PktRecv:                 int64(stats.pktRecv),
		PktSndLoss:              int(stats.pktSndLoss),
		PktRcvLoss:              int(stats.pktRcvLoss),
		PktRetrans:              int(stats.pktRetrans),
		PktRcvRetrans:           int(stats.pktRcvRetrans),
		PktSentACK:              int(stats.pktSentACK),
		PktRecvACK:              int(stats.pktRecvACK),
		PktSentNAK:              int(stats.pktSentNAK),
		PktRecvNAK:              int(stats.pktRecvNAK),
		MbpsSendRate:            float64(stats.mbpsSendRate),
		MbpsRecvRate:            float64(stats.mbpsRecvRate),
		UsSndDuration:           int64(stats.usSndDuration),
		PktReorderDistance:      int(stats.pktReorderDistance),
		PktRcvAvgBelatedTime:    float64(stats.pktRcvAvgBelatedTime),
		PktRcvBelated:           int64(stats.pktRcvBelated),
		PktSndDrop:              int(stats.pktSndDrop),
		PktRcvDrop:              int(stats.pktRcvDrop),
		PktRcvUndecrypt:         int(stats.pktRcvUndecrypt),
		ByteSent:                uint64(stats.byteSent),
		ByteRecv:                uint64(stats.byteRecv),
		ByteRcvLoss:             uint64(stats.byteRcvLoss),
		ByteRetrans:             uint64(stats.byteRetrans),
		ByteSndDrop:             uint64(stats.byteSndDrop),
		ByteRcvDrop:             uint64(stats.byteRcvDrop),
		ByteRcvUndecrypt:        uint64(stats.byteRcvUndecrypt),
		UsPktSndPeriod:          float64(stats.usPktSndPeriod),
		PktFlowWindow:           int(stats.pktFlowWindow),
		PktCongestionWindow:     int(stats.pktCongestionWindow),
		PktFlightSize:           int(stats.pktFlightSize),
		MsRTT:                   float64(stats.msRTT),
		MbpsBandwidth:           float64(stats.mbpsBandwidth),
		ByteAvailSndBuf:         int(stats.byteAvailSndBuf),
		ByteAvailRcvBuf:         int(stats.byteAvailRcvBuf),
		MbpsMaxBW:               float64(stats.mbpsMaxBW),
		ByteMSS:                 int(stats.byteMSS),
		PktSndBuf:               int(stats.pktSndBuf),
		ByteSndBuf:              int(stats.byteSndBuf),
		MsSndBuf:                int(stats.msSndBuf),
		MsSndTsbPdDelay:         int(stats.msSndTsbPdDelay),
		PktRcvBuf:               int(stats.pktRcvBuf),
		ByteRcvBuf:              int(stats.byteRcvBuf),
		MsRcvBuf:                int(stats.msRcvBuf),
		MsRcvTsbPdDelay:         int(stats.msRcvTsbPdDelay),
		PktSndFilterExtraTotal:  int(stats.pktSndFilterExtraTotal),
		PktRcvFilterExtraTotal:  int(stats.pktRcvFilterExtraTotal),
		PktRcvFilterSupplyTotal: int(stats.pktRcvFilterSupplyTotal),
		PktRcvFilterLossTotal:   int(stats.pktRcvFilterLossTotal),
		PktSndFilterExtra:       int(stats.pktSndFilterExtra),
		PktRcvFilterExtra:       int(stats.pktRcvFilterExtra),
		PktRcvFilterSupply:      int(stats.pktRcvFilterSupply),
		PktRcvFilterLoss:        int(stats.pktRcvFilterLoss),
		PktReorderTolerance:     int(stats.pktReorderTolerance),
		PktSentUniqueTotal:      int64(stats.pktSentUniqueTotal),
		PktRecvUniqueTotal:      int64(stats.pktRecvUniqueTotal),
		ByteSentUniqueTotal:     uint64(stats.byteSentUniqueTotal),
		ByteRecvUniqueTotal:     uint64(stats.byteRecvUniqueTotal),
		PktSentUnique:           int64(stats.pktSentUnique),
		PktRecvUnique:           int64(stats.pktRecvUnique),
		ByteSentUnique:          uint64(stats.byteSentUnique),
		ByteRecvUnique:          uint64(stats.byteRecvUnique),
	}, nil
}
