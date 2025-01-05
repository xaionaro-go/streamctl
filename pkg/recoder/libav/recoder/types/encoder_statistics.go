package types

type EncoderFramesStatistics struct {
	Unparsed         uint64
	VideoUnprocessed uint64
	AudioUnprocessed uint64
	VideoProcessed   uint64
	AudioProcessed   uint64
}

type EncoderStatistics struct {
	BytesCountRead  uint64
	BytesCountWrote uint64
	FramesRead      EncoderFramesStatistics
	FramesWrote     EncoderFramesStatistics
}
