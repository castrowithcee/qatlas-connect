package aws

// The tables name guards and readbacks, and the files register confirmation texts, only for the tools an
// expected_<service>_test.go lists, one per line, so a new step is a deliberate entry in the file of its service.
var expectedGuarded, expectedReadback, expectedSummary = map[string]bool{}, map[string]bool{}, map[string]bool{}

func expectGuarded(ids ...string) {
	for _, id := range ids {
		expectedGuarded[id] = true
	}
}

func expectReadback(ids ...string) {
	for _, id := range ids {
		expectedReadback[id] = true
	}
}

func expectSummary(ids ...string) {
	for _, id := range ids {
		expectedSummary[id] = true
	}
}
