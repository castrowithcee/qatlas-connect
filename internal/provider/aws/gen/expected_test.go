package gen

// The tables name guards and readbacks only for the operations an expected_<service>_test.go lists, one per
// line, so a new step is a deliberate entry in the file of its service.
var expectedGuarded, expectedReadback = map[string]bool{}, map[string]bool{}

func expectGuarded(ops ...string) {
	for _, op := range ops {
		expectedGuarded[op] = true
	}
}

func expectReadback(ops ...string) {
	for _, op := range ops {
		expectedReadback[op] = true
	}
}
