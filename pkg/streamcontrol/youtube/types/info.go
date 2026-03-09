package youtube

import (
	"sync/atomic"
	"time"

	"github.com/xaionaro-go/xsync"
)

const (
	YouTubeDailyQuotaLimit = 10000
)

type YouTubeInfo struct {
	QuotaUsage         *QuotaUsage
	ChatListeners      []ChatListenerInfo
	ActiveBroadcasts   []BroadcastSummary
	UpcomingBroadcasts []BroadcastSummary
}

type QuotaUsage struct {
	UsedPoints              atomic.Uint64
	PerOperationUsage        xsync.Map[string, uint64]
	PerOperationRequestCount xsync.Map[string, uint64]
	DailyLimit               uint64
	ResetTime                time.Time

	GoogleReportedUsage *uint64
	GoogleReportedLimit *uint64
	GoogleReportedAt    *time.Time
}

type ChatListenerInfo struct {
	VideoID  string
	ChatID   string
	IsActive bool
}

type BroadcastSummary struct {
	ID             string
	Title          string
	Status         string
	ScheduledStart time.Time
	ActualStart    time.Time
	ViewerCount    uint64
}
