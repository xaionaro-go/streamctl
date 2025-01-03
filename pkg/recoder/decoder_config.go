package recoder

type HardwareDeviceTypeName string

type DecoderConfig struct {
	CodecName              CodecName
	HardwareDeviceTypeName HardwareDeviceTypeName
}
