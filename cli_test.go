package mping

import "testing"

func TestParseCidr(t *testing.T) {
	parseTests := []struct {
		testcase []string
		want     []string
	}{
		{[]string{"192.168.1.0", "localhost"}, []string{"192.168.1.0", "localhost"}},
		{[]string{"192.168.1.0/32", "localhost"}, []string{"192.168.1.0", "localhost"}},
		{[]string{"192.168.1.0/30", "localhost"}, []string{"192.168.1.0", "192.168.1.1", "192.168.1.2", "192.168.1.3", "localhost"}},
	}

	for _, v := range parseTests {
		actual := parseCidr(v.testcase)

		if len(v.want) != len(actual) {
			t.Errorf("lossTest. actual: %v want: %v", actual, v.want)
		}
		for i := range actual {
			if actual[i] != v.want[i] {
				t.Errorf("lossTest. actual: %v want: %v", actual, v.want)
			}
		}
	}
}
