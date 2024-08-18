package consts

type varKeyPrefix string

const (
	PrefixVarKeyImage = varKeyPrefix("image/")
)

type ImageID string

const (
	ImageChat     = ImageID("chat")
	ImageCounters = ImageID("counters")

	ImageMonitorScreenHighQualityForeground = ImageID("monitor_screen_high_quality_foreground")
	ImageMonitorScreenLowQualityBackground  = ImageID("monitor_screen_low_quality_background")
)

type VarKey string

func VarKeyImage(imageID ImageID) VarKey {
	return VarKey(PrefixVarKeyImage) + VarKey(imageID)
}
