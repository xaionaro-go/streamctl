package kick

import (
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/kickcom"
)

func (cache *Cache) GetChanInfo() *kickcom.ChannelV1 {
	if cache == nil {
		return nil
	}

	return xatomic.LoadPointer(&cache.ChanInfo)
}

func (cache *Cache) SetChanInfo(chanInfo *kickcom.ChannelV1) {
	xatomic.StorePointer(&cache.ChanInfo, chanInfo)
}

func (cache *Cache) GetCategories() []kickcom.CategoryV1Short {
	if cache == nil {
		return nil
	}

	ptr := xatomic.LoadPointer(&cache.Categories)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (cache *Cache) SetCategories(categories []kickcom.CategoryV1Short) {
	xatomic.StorePointer(&cache.Categories, &categories)
}
