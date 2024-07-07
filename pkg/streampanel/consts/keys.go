package consts

type varKeyPrefix string

const (
	PrefixVarKeyImage = varKeyPrefix("image/")
)

type ImageID string

const (
	ImageScreenshot = ImageID("screenshot")
	ImageChat       = ImageID("chat")
)

type VarKey string

func VarKeyImage(imageID ImageID) VarKey {
	return VarKey(PrefixVarKeyImage) + VarKey(imageID)
}
