package policy

import (
	"fmt"
	"testing"
)

func BenchmarkDiffScale(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		for _, expanded := range []bool{false, true} {
			name := "allow"
			if expanded {
				name = "deny"
			}
			b.Run(fmt.Sprintf("entries=%d/%s", size, name), func(b *testing.B) {
				e := engineForTest(b)
				p, c := pair()
				p.AllowedActions = make([]string, size)
				p.Resources["orders"] = make([]string, size)
				for i := range size {
					p.AllowedActions[i] = fmt.Sprintf("action.%06d", i)
					p.Resources["orders"][i] = fmt.Sprintf("order-%06d", i)
				}
				c.AllowedActions = append([]string(nil), p.AllowedActions...)
				c.Resources["orders"] = append([]string(nil), p.Resources["orders"]...)
				want := "ALLOW"
				if expanded {
					c.Resources["orders"][size-1] = "outside-policy"
					want = "DENY"
				}
				if result := e.Diff(p, c, testNow); result.Decision != want {
					b.Fatalf("invalid fixture: %+v", result)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if result := e.Diff(p, c, testNow); result.Decision != want {
						b.Fatal("incorrect decision under load")
					}
				}
			})
		}
	}
}
