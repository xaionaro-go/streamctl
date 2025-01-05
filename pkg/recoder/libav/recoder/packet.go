package recoder

import (
	"github.com/asticode/go-astiav"
)

var PacketPool = newPool(
	astiav.AllocPacket,
	func(p *astiav.Packet) { p.Unref() },
	func(p *astiav.Packet) { p.Free() },
)

func CopyPacketWritable(dst, src *astiav.Packet) {
	dst.Ref(src)
	err := dst.MakeWritable()
	if err != nil {
		panic(err)
	}
}

func ClonePacketAsWritable(src *astiav.Packet) *astiav.Packet {
	dst := PacketPool.Get()
	CopyPacketWritable(dst, src)
	return dst
}
