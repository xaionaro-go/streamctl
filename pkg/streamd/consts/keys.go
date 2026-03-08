package consts

type varKeyPrefix string

const (
	PrefixVarKeyImage = varKeyPrefix("image/")
)

type ImageID string

type VarKey string

func VarKeyImage(imageID ImageID) VarKey {
	return VarKey(PrefixVarKeyImage) + VarKey(imageID)
}

type AlignX string

const (
	UndefinedAlignX = AlignX("")
	AlignXLeft      = AlignX("left")
	AlignXMiddle    = AlignX("middle")
	AlignXRight     = AlignX("right")
)

type AlignY string

const (
	UndefinedAlignY = AlignY("")
	AlignYTop       = AlignY("top")
	AlignYMiddle    = AlignY("middle")
	AlignYBottom    = AlignY("bottom")
)
