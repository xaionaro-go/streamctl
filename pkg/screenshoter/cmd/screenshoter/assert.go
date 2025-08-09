package main

func assertNoError(err error) {
	if err != nil {
		panic(err)
	}
}
