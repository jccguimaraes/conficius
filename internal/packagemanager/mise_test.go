package packagemanager

import "testing"

func TestEquivalentMiseVersionsIgnoreVPrefix(t *testing.T) {
	if !equivalentMiseVersions("v2.13.2", "2.13.2") {
		t.Fatal("v-prefixed version should be equivalent")
	}
	if equivalentMiseVersions("v2.13.1", "2.13.2") {
		t.Fatal("different versions should not be equivalent")
	}
}
