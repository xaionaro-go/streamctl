package streamd

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/chai2010/webp"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/consts"
	"github.com/xaionaro-go/xsync"
)

func (d *StreamD) getImageBytes(
	ctx context.Context,
	elName string,
	el config.DashboardElementConfig,
	dataProvider config.ImageDataProvider,
) ([]byte, time.Time, error) {

	src := el.Source
	if getImageByteser, ok := src.(config.GetImageBytes); ok {
		bytes, _, nextUpdateAt, err := getImageByteser.GetImageBytes(ctx, el, dataProvider)
		if err != nil {
			return nil, time.Now().Add(time.Second), fmt.Errorf("unable to get the image from the source using GetImageByteser: %w", err)
		}
		return bytes, nextUpdateAt, nil
	}

	img, nextUpdateAt, err := el.Source.GetImage(ctx, el, dataProvider)
	if err != nil {
		return nil, time.Now().Add(time.Second), fmt.Errorf("unable to get the image from the source: %w", err)
	}

	if imgHash, err := newImageHash(img); err == nil {
		if imgOldHash, ok := d.ImageHash.Swap(elName, imgHash); ok {
			if imgHash == imgOldHash {
				return nil, nextUpdateAt, ErrNotChanged
			}
		}
	}

	for _, filter := range el.Filters {
		img = filter.Filter(ctx, img)
	}

	var out bytes.Buffer
	err = webp.Encode(&out, img, &webp.Options{
		Lossless: el.ImageLossless,
		Quality:  float32(el.ImageQuality),
		Exact:    false,
	})
	if err != nil {
		return nil, time.Now().Add(time.Second), fmt.Errorf("unable to encode the image: %w", err)
	}

	return out.Bytes(), nextUpdateAt, nil
}

func (d *StreamD) initImageTaker(ctx context.Context) error {
	observability.Go(ctx, func(ctx context.Context) {
		ctxCh, cancelFn := context.WithCancel(ctx)
		defer cancelFn()
		defer logger.Debugf(ctx, "/imageTaker")
		ch, restartCh, err := autoResubscribe(ctxCh, d.SubscribeToDashboardChanges)
		if err != nil {
			logger.Errorf(ctx, "unable to subscribe to dashboard changes: %v", err)
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-restartCh:
				d.restartImageTaker(ctx)
			case <-ch:
				d.restartImageTaker(ctx)
			}
		}
	})

	return d.restartImageTaker(ctx)
}

func (d *StreamD) restartImageTaker(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &d.imageTakerLocker, d.restartImageTakerNoLock, ctx)
}

func (d *StreamD) restartImageTakerNoLock(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "restartImageTakerNoLock")
	defer func() { logger.Debugf(ctx, "/restartImageTakerNoLock: %v", _err) }()
	if d.imageTakerCancel != nil {
		d.imageTakerCancel()
		d.imageTakerCancel = nil
		d.imageTakerWG.Wait()
	}

	ctx, cancelFn := context.WithCancel(ctx)
	d.imageTakerCancel = cancelFn

	for elName, el := range d.Config.Dashboard.Elements {
		if el.Source == nil {
			continue
		}
		if _, ok := el.Source.(*config.DashboardSourceImageDummy); ok {
			continue
		}
		{
			elName, el := elName, el
			_ = el
			d.imageTakerWG.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
				defer d.imageTakerWG.Done()
				logger.Debugf(ctx, "taker of image '%s'", elName)
				defer logger.Debugf(ctx, "/taker of image '%s'", elName)

				obsServer, obsServerClose, err := d.OBS(ctx)
				if obsServerClose != nil {
					defer obsServerClose()
				}
				if err != nil {
					logger.Errorf(ctx, "unable to init connection with OBS: %v", err)
					return
				}

				imageDataProvider := newImageDataProvider(d, obsServer)

				for {
					var (
						imgBytes     []byte
						nextUpdateAt time.Time
						err          error
					)

					waitUntilNextIteration := func() bool {
						if nextUpdateAt.IsZero() {
							return false
						}
						select {
						case <-ctx.Done():
							return false
						case <-time.After(time.Until(nextUpdateAt)):
							return true
						}
					}

					imgBytes, nextUpdateAt, err = d.getImageBytes(ctx, elName, el, imageDataProvider)
					if err != nil {
						if err != ErrNotChanged {
							logger.Tracef(ctx, "the image have not changed of '%s'", elName)
						} else {
							logger.Errorf(ctx, "unable to get the image of '%s': %v", elName, err)
						}
						if !waitUntilNextIteration() {
							return
						}
						continue
					}

					err = d.SetVariable(ctx, consts.VarKeyImage(consts.ImageID(elName)), imgBytes)
					if err != nil {
						logger.Errorf(ctx, "unable to save the image of '%s': %v", elName, err)
						if !waitUntilNextIteration() {
							return
						}
						continue
					}

					if !waitUntilNextIteration() {
						return
					}
				}
			})
		}
	}
	return nil
}
