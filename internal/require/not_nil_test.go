package require

import "testing"

type testDependency struct{}

func TestNotNilPanicsOnTypedNil(t *testing.T) {
	var dependency *testDependency
	defer func() {
		if recover() == nil {
			t.Fatal("NotNil() did not panic")
		}
	}()
	NotNil("dependency", dependency)
}

func TestNotNilAcceptsNonNil(t *testing.T) {
	NotNil("dependency", &testDependency{})
}
