package kick

import (
	"github.com/go-ng/xatomic"
	"github.com/scorfly/gokick"
)

func (cache *Cache) GetChanInfo() *gokick.ChannelResponse {
	if cache == nil {
		return nil
	}

	return xatomic.LoadPointer(&cache.ChanInfo)
}

func (cache *Cache) SetChanInfo(chanInfo *gokick.ChannelResponse) {
	xatomic.StorePointer(&cache.ChanInfo, chanInfo)
}

func (cache *Cache) GetCategories() []gokick.CategoryResponse {
	if cache == nil {
		return nil
	}

	ptr := xatomic.LoadPointer(&cache.Categories)
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (cache *Cache) SetCategories(categories []gokick.CategoryResponse) {
	xatomic.StorePointer(&cache.Categories, &categories)
}
