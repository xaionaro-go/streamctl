package recoder

import (
	"fmt"
	"unsafe"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/libsrt/threadsafe"
	"github.com/xaionaro-go/unsafetools"
)

func formatContextToSRTSocket(fmtCtx *astiav.FormatContext) (*threadsafe.Socket, error) {
	privData := fmtCtx.PrivateData()
	if privData == nil {
		return nil, fmt.Errorf("failed to get 'priv_data'")
	}

	privDataCP := *unsafetools.FieldByName(privData, "c").(*unsafe.Pointer)
	privDataC := int32(uintptr(privDataCP))

	sock := threadsafe.SocketFromC(privDataC)
	return sock, nil
}
