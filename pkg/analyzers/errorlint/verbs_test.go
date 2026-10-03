package errorlint

import "testing"

func TestErrorVerbs(t *testing.T) {
	cases := []struct {
		format string
		want   []int // 0-based operand indexes of %s and %v
	}{
		{format: "%v", want: []int{0}},
		{format: "%s", want: []int{0}},
		{format: "%w", want: nil},
		{format: "%[1]v", want: []int{0}},
		{format: "%[2]v", want: []int{1}},
		{format: "%[2]v %[1]s", want: []int{1, 0}},
		{format: "%d %v", want: []int{1}},
		{format: "%*d %v", want: []int{2}},
		{format: "%*s", want: []int{1}},
		{format: "%.*s", want: []int{1}},
		{format: "%*.*s", want: []int{2}},
		{format: "100%% %v", want: []int{0}},
		{format: "%[1]d %[2]v", want: []int{1}},
		{format: "a\n%v", want: []int{0}},
	}
	for _, tc := range cases {
		got := errorVerbs(tc.format)
		if len(got) != len(tc.want) {
			t.Errorf("%q: got %d verbs %#v, want indexes %v", tc.format, len(got), got, tc.want)
			continue
		}
		for i := range got {
			if got[i].index != tc.want[i] {
				t.Errorf("%q: verb %d index %d, want %d", tc.format, i, got[i].index, tc.want[i])
			}
		}
	}
}
