package streamserver

func assert(b bool) {
	if !b {
		panic("assertion failed")
	}
}
