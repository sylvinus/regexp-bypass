package main

import (
	"bufio"
	"fmt"
	regexpb "github.com/sylvinus/regexp-bypass/regexp"
	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

var pattern = `x.xy$`
var minN = 5
var maxN = 50
var step = 5
var re = regexp.MustCompile(pattern)
var rebypass = regexpb.MustCompile(pattern)

func getString(n int) string {
	return strings.Repeat("x", n) + "y"
}

func main() {

	seriesLibrary := chart.ContinuousSeries{
		Name:    "Library",
		XValues: []float64{},
		YValues: []float64{},
		Style: chart.Style{
			Show:        true,
			StrokeColor: drawing.ColorRed,
		},
	}
	seriesBypass := chart.ContinuousSeries{
		Name:    "Bypass",
		XValues: []float64{},
		YValues: []float64{},
	}

	for i := minN; i < maxN; i += step {

		text := getString(i)

		resLibrary := testing.Benchmark(func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				re.MatchString(text)
			}
		})

		resBypass := testing.Benchmark(func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				rebypass.MatchString(text)
			}
		})

		fmt.Printf("\nBenchmarking string length=%d\n", i)
		fmt.Printf("Library: %[1]s\n%#[1]v\n", resLibrary)
		fmt.Printf("Bypass:  %[1]s\n%#[1]v\n", resBypass)

		seriesLibrary.XValues = append(seriesLibrary.XValues, float64(i))
		seriesLibrary.YValues = append(seriesLibrary.YValues, float64(resLibrary.T/time.Nanosecond)/float64(resLibrary.N))

		seriesBypass.XValues = append(seriesBypass.XValues, float64(i))
		seriesBypass.YValues = append(seriesBypass.YValues, float64(resBypass.T/time.Nanosecond)/float64(resBypass.N))

	}

	graph := chart.Chart{
		Series: []chart.Series{
			seriesLibrary,
			seriesBypass,
		},
		XAxis: chart.XAxis{
			NameStyle: chart.StyleShow(),
			Style:     chart.StyleShow(),
		},
		YAxis: chart.YAxis{
			NameStyle: chart.StyleShow(),
			Style:     chart.StyleShow(),
			ValueFormatter: func(v interface{}) string {
				if vf, isFloat := v.(float64); isFloat {
					return fmt.Sprintf("%0.0f ns", vf)
				}
				return ""
			},
		},
		Background: chart.Style{
			Padding: chart.Box{
				Top:  20,
				Left: 20,
			},
		},
	}

	graph.Elements = []chart.Renderable{
		chart.Legend(&graph),
	}

	f, _ := os.Create("benchmark_chart/output.png")
	defer f.Close()

	w := bufio.NewWriter(f)

	graph.Render(chart.PNG, w)
	w.Flush()
}
