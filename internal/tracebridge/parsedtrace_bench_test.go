package tracebridge

import (
	"fmt"
	"strings"
	"testing"
)

// genParsedTrace produces a valid parsed trace string with nGoroutines goroutines.
func genParsedTrace(nGoroutines int) string {
	var b strings.Builder
	b.WriteString("M=-1 P=-1 G=-1 Sync Time=100 N=1 Trace=101 Mono=100 Wall=2026-03-14T12:00:00Z\n")
	b.WriteString("M=1 P=0 G=-1 StateTransition Time=100 Resource=Goroutine(1) Reason=\"\" GoID=1 Undetermined->Running\n")
	b.WriteString("Stack=\n\tmain.main @ 0x1\n\t\t/work/main.go:10\n\n")

	for i := 2; i <= nGoroutines; i++ {
		parent := 1
		if i > 2 {
			parent = i - 1
		}
		t := 100 + i*10
		b.WriteString(fmt.Sprintf("M=1 P=0 G=%d StateTransition Time=%d Resource=Goroutine(%d) Reason=\"\" GoID=%d NotExist->Runnable\n", parent, t, i, i))
		b.WriteString(fmt.Sprintf("Stack=\n\tmain.worker%d @ 0x%d\n\t\t/work/main.go:%d\n\n", i, i, 20+i))
		b.WriteString(fmt.Sprintf("M=1 P=0 G=%d StateTransition Time=%d Resource=Goroutine(%d) Reason=\"\" GoID=%d Runnable->Running\n", parent, t+5, i, i))
		b.WriteString(fmt.Sprintf("Stack=\n\tmain.worker%d @ 0x%d\n\t\t/work/main.go:%d\n\n", i, i, 20+i))
	}

	b.WriteString("M=1 P=0 G=1 StateTransition Time=99999 Resource=Goroutine(1) Reason=\"\" GoID=1 Running->NotExist\n")
	return b.String()
}

func BenchmarkParseParsedTrace(b *testing.B) {
	sizes := []int{100, 1000, 5000} // NFR target: 10k–20k goroutines
	for _, n := range sizes {
		n := n
		trace := genParsedTrace(n)
		b.Run(fmt.Sprintf("goroutines=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := ParseParsedTrace(strings.NewReader(trace))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
