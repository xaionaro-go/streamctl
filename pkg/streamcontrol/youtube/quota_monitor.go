package youtube

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"github.com/facebookincubator/go-belt/tool/logger"
	"golang.org/x/oauth2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	googleQuotaCacheTTL = 5 * time.Minute

	quotaUsageMetricFilter = `metric.type="serviceruntime.googleapis.com/quota/allocation/usage"` +
		` AND resource.type="consumer_quota"` +
		` AND resource.label.service="youtube.googleapis.com"`

	quotaLimitMetricFilter = `metric.type="serviceruntime.googleapis.com/quota/limit"` +
		` AND resource.type="consumer_quota"` +
		` AND resource.label.service="youtube.googleapis.com"`
)

type googleQuotaCache struct {
	mu        sync.Mutex
	usage     uint64
	limit     uint64
	fetchedAt time.Time
	valid     bool
	nowFunc   func() time.Time // for testing; nil defaults to time.Now
}

func (c *googleQuotaCache) now() time.Time {
	if c.nowFunc != nil {
		return c.nowFunc()
	}
	return time.Now()
}

func (c *googleQuotaCache) isExpired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.valid {
		return true
	}
	return c.now().Sub(c.fetchedAt) > googleQuotaCacheTTL
}

func (c *googleQuotaCache) set(
	usage uint64,
	limit uint64,
	fetchedAt time.Time,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.usage = usage
	c.limit = limit
	c.fetchedAt = fetchedAt
	c.valid = true
}

func (c *googleQuotaCache) get() (usage, limit uint64, fetchedAt time.Time, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.valid {
		return 0, 0, time.Time{}, false
	}
	return c.usage, c.limit, c.fetchedAt, true
}

func fetchGoogleQuota(
	ctx context.Context,
	projectID string,
	tokenSource oauth2.TokenSource,
) (usage uint64, limit uint64, _err error) {
	logger.Debugf(ctx, "fetchGoogleQuota")
	defer func() { logger.Debugf(ctx, "/fetchGoogleQuota: %v", _err) }()

	client, err := monitoring.NewMetricClient(ctx,
		option.WithTokenSource(tokenSource),
	)
	if err != nil {
		return 0, 0, fmt.Errorf("unable to create monitoring client: %w", err)
	}
	defer client.Close()

	now := time.Now()
	dayAgo := now.Add(-24 * time.Hour)
	interval := &monitoringpb.TimeInterval{
		StartTime: timestamppb.New(dayAgo),
		EndTime:   timestamppb.New(now),
	}
	projectName := "projects/" + projectID

	usage, err = fetchLatestGaugeValue(ctx, client, projectName, quotaUsageMetricFilter, interval)
	if err != nil {
		return 0, 0, fmt.Errorf("unable to fetch quota usage: %w", err)
	}

	limit, err = fetchLatestGaugeValue(ctx, client, projectName, quotaLimitMetricFilter, interval)
	if err != nil {
		return 0, 0, fmt.Errorf("unable to fetch quota limit: %w", err)
	}

	return usage, limit, nil
}

func fetchLatestGaugeValue(
	ctx context.Context,
	client *monitoring.MetricClient,
	projectName string,
	filter string,
	interval *monitoringpb.TimeInterval,
) (uint64, error) {
	it := client.ListTimeSeries(ctx, &monitoringpb.ListTimeSeriesRequest{
		Name:     projectName,
		Filter:   filter,
		Interval: interval,
		View:     monitoringpb.ListTimeSeriesRequest_FULL,
	})

	var latestValue int64
	var latestTime time.Time
	var found bool

	for {
		ts, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("unable to iterate time series: %w", err)
		}

		for _, point := range ts.Points {
			pointTime := point.Interval.EndTime.AsTime()
			if pointTime.After(latestTime) {
				latestTime = pointTime
				latestValue = point.Value.GetInt64Value()
				found = true
			}
		}
	}

	if !found {
		return 0, fmt.Errorf("no data points found for filter %q", filter)
	}

	if latestValue < 0 {
		latestValue = 0
	}
	return uint64(latestValue), nil
}
